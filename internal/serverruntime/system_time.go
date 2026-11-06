package serverruntime

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

type SystemTimeStatus struct {
	Unix         int64  `json:"unix"`
	Timezone     string `json:"timezone"`
	Status       string `json:"status"`
	NTPEnabled   bool   `json:"ntp_enabled"`
	Synchronized bool   `json:"synchronized"`
	Supported    bool   `json:"supported"`
	CheckedAt    int64  `json:"checked_at"`
	Syncing      bool   `json:"syncing"`
	Error        string `json:"error,omitempty"`
}

func (runtime *Runtime) clockCommand(ctx context.Context, args ...string) ([]byte, error) {
	if runtime.timeCommand != nil {
		return runtime.timeCommand(ctx, args...)
	}
	return exec.CommandContext(ctx, "timedatectl", args...).Output()
}

func (runtime *Runtime) systemTimeSnapshot() SystemTimeStatus {
	runtime.timeMu.RLock()
	value := runtime.timeState
	runtime.timeMu.RUnlock()
	value.Unix = time.Now().Unix()
	if value.Timezone == "" {
		value.Timezone = "UTC"
	}
	if value.Status == "" {
		value.Status = "checking"
	}
	if year := time.Now().Year(); year < 2024 || year > 2100 {
		value.Status = "invalid_date"
	}
	return value
}

func (runtime *Runtime) checkSystemTime(ctx context.Context) {
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := runtime.clockCommand(check, "show", "--property=Timezone", "--property=NTP", "--property=NTPSynchronized", "--property=CanNTP")
	value := SystemTimeStatus{CheckedAt: time.Now().Unix(), Timezone: "UTC", Status: "unavailable"}
	if err != nil {
		value.Error = "Не удалось проверить службу времени"
	} else {
		fields := map[string]string{}
		for _, line := range strings.Split(string(output), "\n") {
			key, val, ok := strings.Cut(line, "=")
			if ok {
				fields[key] = strings.TrimSpace(val)
			}
		}
		value.Timezone = fields["Timezone"]
		value.NTPEnabled = fields["NTP"] == "yes"
		value.Supported = fields["CanNTP"] == "yes"
		value.Synchronized = fields["NTPSynchronized"] == "yes"
		if fields["NTPSynchronized"] == "yes" {
			value.Status = "synchronized"
		} else if fields["NTPSynchronized"] == "no" {
			value.Status = "unsynchronized"
		}
	}
	runtime.timeMu.Lock()
	value.Syncing = runtime.timeState.Syncing
	runtime.timeState = value
	runtime.timeMu.Unlock()
}

func (runtime *Runtime) RunSystemTimeMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		runtime.checkSystemTime(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (runtime *Runtime) syncSystemTime() (int, any) {
	runtime.timeMu.Lock()
	if runtime.timeState.Syncing {
		runtime.timeMu.Unlock()
		return 202, map[string]any{"accepted": true}
	}
	runtime.timeState.Syncing = true
	runtime.timeState.Error = ""
	runtime.timeMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		// Ask the existing system NTP provider; never set an arbitrary client
		// date, disable NTP, or restart an unrelated time service.
		_, err := runtime.clockCommand(ctx, "set-ntp", "true")
		runtime.checkSystemTime(ctx)
		runtime.timeMu.Lock()
		runtime.timeState.Syncing = false
		if err != nil {
			runtime.timeState.Error = "Не удалось включить синхронизацию времени"
		}
		runtime.timeMu.Unlock()
	}()
	return 202, map[string]any{"accepted": true}
}
