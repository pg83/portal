package main

import (
	"os"
	"fmt"
	"log"
	"os/exec"
	"strings"
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
	conn *dbus.Conn
}

type request struct {
	conn *dbus.Conn
	path dbus.ObjectPath
}

func newRequest(conn *dbus.Conn, sender string, token string) *request {
	sender, _ = strings.CutPrefix(sender, ":")
	sender = strings.ReplaceAll(sender, ".", "_")

	path := fmt.Sprintf("/org/freedesktop/portal/desktop/request/%s/%s", sender, token)

	return &request{
		conn: conn,
		path: dbus.ObjectPath(path),
	}
}

func (r *request) response(errcode uint32, results kv) {
	err := r.conn.Emit(r.path, "org.freedesktop.portal.Request.Response", errcode, results)

	if err != nil {
		fmtException("can not send response: %v", err).throw()
	}
}

func (p *portal) OpenURI(parent string, uri string, options *kv) *dbus.Error {
	log.Println("enter OpenURI", parent, uri, options)

	go func() {
		try(func() {
			xdgOpen(uri);
		}).catch(func(exc *Exception) {
			log.Println("in OpenURI", exc.what())
		})
	}()

	return nil
}

func (p *portal) OpenFile(sender dbus.Sender, parent string, title string, options kv) (dbus.ObjectPath, *dbus.Error) {
	log.Println("enter OpenFile", sender, parent, title, options)

	tok := options["handle_token"]
	req := newRequest(p.conn, string(sender), tok.Value().(string))

	go func() {
		try(func() {
			pat, err := exec.Command("zenity", "--file-selection").Output()

			if err != nil {
				log.Println(err)
				req.response(1, kv{})
			} else {
				req.response(0, kv{
					"uris": dbus.MakeVariant([]string{
						"file://" + strings.TrimSpace(string(pat)),
					}),
				})
			}
		}).catch(func(exc *Exception) {
			log.Println("in OpenFile", exc.what())
		})
	}()

	return req.path, nil
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

	p := &portal{
		conn: conn,
	}

	conn.Export(p, "/org/freedesktop/portal/desktop", "org.freedesktop.portal.OpenURI")
	conn.Export(p, "/org/freedesktop/portal/desktop", "org.freedesktop.portal.FileChooser")

	bind(conn, "org.freedesktop.portal.Desktop")

	select {}
}

func main() {
	try(run).catch(func(exc *Exception) {
		exc.fatal(1, "abort")
	})
}
