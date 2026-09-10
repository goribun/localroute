//go:build !darwin && !windows

package main

import "context"

func (a *App) startDesktop(context.Context) {}
func (a *App) stopDesktop()                 {}
