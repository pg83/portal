package main

import (
	"os"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
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

type OpenURI struct {
	portal *portal
}

func (p *OpenURI) OpenURI(parent string, uri string, options *kv) *dbus.Error {
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

type FileChooser struct {
	portal *portal
}

func (p *FileChooser) OpenFile(sender dbus.Sender, parent string, title string, options kv) (dbus.ObjectPath, *dbus.Error) {
	log.Println("enter OpenFile", sender, parent, title, options)

	tok := options["handle_token"]
	req := newRequest(p.portal.conn, string(sender), tok.Value().(string))

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

type Settings struct {
	portal *portal
}

func box(v interface{}) *dbus.Variant {
	if v == nil {
		return nil
	}

	res := dbus.MakeVariant(v)

	return &res
}

func (p *Settings) ReadOne(sender dbus.Sender, namespace string, key string) (*dbus.Variant, *dbus.Error) {
	log.Println("enter ReadOne", sender, namespace, key)

	path := namespace + "." + key

	if path == "org.freedesktop.appearance.color-scheme" {
		return box(uint32(1)), nil
	}

	return nil, &dbus.ErrMsgNoObject
}

func (p *Settings) Read(sender dbus.Sender, namespace string, key string) (*dbus.Variant, *dbus.Error) {
	res, err := p.ReadOne(sender, namespace, key)

	return box(res), err
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

	path := dbus.ObjectPath("/org/freedesktop/portal/desktop")

	portal := &portal{
		conn: conn,
	}

	ou := &OpenURI{
		portal: portal,
	}

	conn.Export(ou, path, "org.freedesktop.portal.OpenURI")

	fc := &FileChooser{
		portal: portal,
	}

	conn.Export(fc, path, "org.freedesktop.portal.FileChooser")

	st := &Settings{
		portal: portal,
	}

	conn.Export(st, path, "org.freedesktop.portal.Settings")

	props := map[string]map[string]*prop.Prop{
		"org.freedesktop.portal.OpenURI": {
			"version": {
				Value: uint32(4),
			},
		},
		"org.freedesktop.portal.FileChooser": {
			"version": {
				Value: uint32(3),
			},
		},
		"org.freedesktop.portal.Settings": {
			"version": {
				Value: uint32(1),
			},
		},
	}

	_, err := prop.Export(conn, path, props)

	if err != nil {
		fmtException("can not bind properties: %w", err).throw()
	}

	bind(conn, "org.freedesktop.portal.Desktop")

	select {}
}

func main() {
	try(run).catch(func(exc *Exception) {
		exc.fatal(1, "abort")
	})
}
