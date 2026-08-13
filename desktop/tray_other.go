//go:build !linux

package main

import "context"

func startDesktopTray(context.Context) {}
func stopDesktopTray()                 {}
