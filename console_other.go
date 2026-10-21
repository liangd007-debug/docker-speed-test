//go:build !windows

package main

func classifyNetErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var outEnc *struct{}

func initConsole(mode string)  { outEnc = nil }
func enableColor() bool      { return false }
func encode(s string) []byte { return []byte(s) }
func hasConsole() bool       { return true }
