package main

import (
	"net"
	"net/netip"
	"net/url"
	"os/exec"
	"slices"
	"strconv"

	"github.com/atotto/clipboard"
)

// openCommand opens a URL with the desktop's default handler.
const openCommand = "xdg-open"

// localURL returns the URL a browser on this machine would use to reach the
// row's port: localhost when it listens on every interface, otherwise the
// address it is bound to.
func localURL(r row) string {
	host := net.JoinHostPort("localhost", strconv.Itoa(int(r.Port)))
	if len(r.Addrs) > 0 && !slices.ContainsFunc(r.Addrs, netip.Addr.IsUnspecified) {
		host = netip.AddrPortFrom(r.Addrs[0], r.Port).String()
	}
	return (&url.URL{Scheme: "http", Host: host}).String()
}

// openURL opens u in the default browser without waiting for it.
func openURL(u string) error {
	cmd := exec.Command(openCommand, u)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap the opener once it hands the URL to the browser
	return nil
}

// copyText puts s on the system clipboard using the desktop's clipboard
// tool (wl-copy, xclip or xsel).
func copyText(s string) error {
	return clipboard.WriteAll(s)
}
