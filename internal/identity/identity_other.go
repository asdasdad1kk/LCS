//go:build !windows

package identity

func osUPN() string     { return "" }
func whoamiUPN() string { return "" }
