//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#include <stdlib.h>

extern void orcherouteTrayOpen(void);
extern void orcherouteTrayQuit(void);

static GtkStatusIcon *orcheroute_status_icon = NULL;
static GtkWidget *orcheroute_tray_menu = NULL;

static void orcheroute_open_cb(GtkMenuItem *item, gpointer data) {
    orcherouteTrayOpen();
}

static void orcheroute_quit_cb(GtkMenuItem *item, gpointer data) {
    orcherouteTrayQuit();
}

static void orcheroute_popup_cb(GtkStatusIcon *icon, guint button, guint activate_time, gpointer data) {
    gtk_menu_popup(GTK_MENU(orcheroute_tray_menu), NULL, NULL,
        gtk_status_icon_position_menu, icon, button, activate_time);
}

static gboolean orcheroute_create_tray(gpointer raw_path) {
    char *path = (char *)raw_path;
    if (orcheroute_status_icon == NULL) {
        orcheroute_status_icon = gtk_status_icon_new_from_file(path);
        gtk_status_icon_set_tooltip_text(orcheroute_status_icon, "OrcheRoute");
        gtk_status_icon_set_visible(orcheroute_status_icon, TRUE);

        orcheroute_tray_menu = gtk_menu_new();
        GtkWidget *open_item = gtk_menu_item_new_with_label("Открыть OrcheRoute");
        GtkWidget *quit_item = gtk_menu_item_new_with_label("Выйти");
        gtk_menu_shell_append(GTK_MENU_SHELL(orcheroute_tray_menu), open_item);
        gtk_menu_shell_append(GTK_MENU_SHELL(orcheroute_tray_menu), quit_item);
        gtk_widget_show_all(orcheroute_tray_menu);

        g_signal_connect(orcheroute_status_icon, "activate", G_CALLBACK(orcheroute_open_cb), NULL);
        g_signal_connect(orcheroute_status_icon, "popup-menu", G_CALLBACK(orcheroute_popup_cb), NULL);
        g_signal_connect(open_item, "activate", G_CALLBACK(orcheroute_open_cb), NULL);
        g_signal_connect(quit_item, "activate", G_CALLBACK(orcheroute_quit_cb), NULL);
    }
    free(path);
    return G_SOURCE_REMOVE;
}

static gboolean orcheroute_destroy_tray(gpointer data) {
    if (orcheroute_status_icon != NULL) {
        gtk_status_icon_set_visible(orcheroute_status_icon, FALSE);
        g_object_unref(orcheroute_status_icon);
        orcheroute_status_icon = NULL;
    }
    if (orcheroute_tray_menu != NULL) {
        gtk_widget_destroy(orcheroute_tray_menu);
        orcheroute_tray_menu = NULL;
    }
    return G_SOURCE_REMOVE;
}

static void orcheroute_tray_start(const char *path) {
    g_idle_add(orcheroute_create_tray, g_strdup(path));
}

static void orcheroute_tray_stop(void) {
    g_idle_add(orcheroute_destroy_tray, NULL);
}
*/
import "C"

import (
	"context"
	"sync"
	"unsafe"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var desktopTray struct {
	sync.RWMutex
	ctx context.Context
}

func startDesktopTray(ctx context.Context) {
	desktopTray.Lock()
	desktopTray.ctx = ctx
	desktopTray.Unlock()
	path := C.CString("/usr/share/icons/hicolor/scalable/apps/orcheroute.svg")
	C.orcheroute_tray_start(path)
	C.free(unsafe.Pointer(path))
}

func stopDesktopTray() {
	C.orcheroute_tray_stop()
	desktopTray.Lock()
	desktopTray.ctx = nil
	desktopTray.Unlock()
}

//export orcherouteTrayOpen
func orcherouteTrayOpen() {
	desktopTray.RLock()
	ctx := desktopTray.ctx
	desktopTray.RUnlock()
	if ctx != nil {
		wailsruntime.WindowShow(ctx)
		wailsruntime.WindowUnminimise(ctx)
	}
}

//export orcherouteTrayQuit
func orcherouteTrayQuit() {
	desktopTray.RLock()
	ctx := desktopTray.ctx
	desktopTray.RUnlock()
	if ctx != nil {
		wailsruntime.Quit(ctx)
	}
}
