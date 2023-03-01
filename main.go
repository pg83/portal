package main

import (
	"os"
	"fmt"
	"os/exec"
	"github.com/godbus/dbus/v5"
)

// exception runtime

type Exception struct {
	what func() error
}

func (self *Exception) throw() {
	panic(self)
}

func (self *Exception) catch(cb func(*Exception)) {
	if self != nil {
		cb(self)
	}
}

func (self *Exception) fatal(code int, prefix string) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", prefix, self.what())
	os.Exit(code)
}

func newException(e error) *Exception {
	return &Exception{
		what: func() error {
			return e
		},
	}
}

func fmtException(format string, args ...any) *Exception {
	return newException(fmt.Errorf(format, args...))
}

func try(cb func()) (err *Exception) {
	defer func() {
		if rec := recover(); rec != nil {
			if exc, ok := rec.(*Exception); ok {
				err = exc
			} else {
				// personality check failed
				panic(rec)
			}
		}
	}()

	cb()

	return nil
}

// end of runtime

type kv map[string]dbus.Variant

func lookPath(prog string) string {
	path, err := exec.LookPath(prog)

	if err != nil {
		fmtException("can not find %s: %v", prog, err).throw()
	}

	return path
}

func xdgOpen(url string) {
	args := []string{"xdg-open-dispatch", url}
	path := lookPath(args[0])

	cmd := &exec.Cmd{
		Path: path,
		Args: args,
	}

	err := cmd.Run()

	if err != nil {
		fmtException("xdg-open-dispatch: %v", err).throw()
	}
}

type portal struct {
}

func (p *portal) OpenURI(parent string, uri string, options *kv) *dbus.Error {
	go func() {
		try(func() {
			fmt.Fprintln(os.Stderr, "OpenURI", parent, uri, options)
			xdgOpen(uri);
		}).catch(func(exc *Exception) {
			fmt.Fprintln(os.Stderr, "OpenURI", exc.what())
		})
	}()

	return nil
}

func bind(conn *dbus.Conn, service string) {
	reply, err := conn.RequestName(service, dbus.NameFlagDoNotQueue)

	if err != nil {
		fmtException("can not request name %s: %w", service, err).throw()
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		fmtException("name %s already taken", service).throw()
	}
}

func sessionBus() *dbus.Conn {
	conn, err := dbus.ConnectSessionBus()

	if err != nil {
		fmtException("can not connect session bus %w", err).throw()
	}

	return conn
}

func run() {
	conn := sessionBus()
	defer conn.Close()

	p := &portal{}

	conn.Export(p, "/org/freedesktop/portal/desktop", "org.freedesktop.portal.OpenURI")

	bind(conn, "org.freedesktop.portal.Desktop")

	select {}
}

func main() {
	try(run).catch(func(exc *Exception) {
		exc.fatal(1, "abort")
	})
}
