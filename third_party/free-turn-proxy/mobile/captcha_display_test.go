package mobile

import "testing"

func TestCaptchaDisplayDefersSecondChallengeUntilAllStreamsLost(t *testing.T) {
	empty := ""
	captchaDisplayUpdate(&empty, 0)
	defer captchaDisplayUpdate(&empty, 0)
	first := "http://localhost:8765/not_robot_captcha?first"
	if value, show := captchaDisplayUpdate(&first, 0); !show || value != first {
		t.Fatal("initial CAPTCHA not shown")
	}
	if value, show := captchaDisplayUpdate(nil, 1); !show || value != "" {
		t.Fatal("connected stream did not close CAPTCHA")
	}
	second := "http://localhost:8765/not_robot_captcha?second"
	if _, show := captchaDisplayUpdate(&second, 1); show {
		t.Fatal("extra CAPTCHA interrupted a working stream")
	}
	if value, show := captchaDisplayUpdate(nil, 0); !show || value != second {
		t.Fatal("pending CAPTCHA not restored after stream loss")
	}
	if _, show := captchaDisplayUpdate(nil, 0); show {
		t.Fatal("duplicate CAPTCHA presentation")
	}
}
