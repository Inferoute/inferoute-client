//go:build !windows

package main

func enableVirtualTerminal() {}

func hideConsole() {}

func showErrorDialog(string) {}

func spawnDetachedIfNeeded() bool { return false }

func isTrayChild() bool { return false }
