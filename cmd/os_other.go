//go:build !windows

package main

func enableVirtualTerminal() {}

func hideConsole() {}

func showErrorDialog(string) {}
