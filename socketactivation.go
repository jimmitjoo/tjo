package tjo

import (
	"errors"
	"net"
	"os"
	"strconv"
)

// Socket activation: accepting a listening socket from systemd instead of
// opening one.
//
// This is what makes `tjo deploy` drop no connections. systemd owns the socket,
// so it survives the process being stopped and started; connections that arrive
// mid-restart queue in the kernel backlog and are served as soon as the new
// binary starts accepting. Nothing coordinates, nothing drains, and there is no
// proxy in the path.
//
// Without it the promise is decoration: the app would open its own socket, and
// the gap between old process closing it and new process binding is a window
// where connections are refused -- while a systemd .socket unit sits alongside
// holding a port nobody is using, so the app would in fact fail to bind at all.
//
// The protocol (sd_listen_fds) is three environment variables and a convention
// that passed descriptors start at 3. That is small enough to implement rather
// than take a dependency for.

// listenFdsStart is the first file descriptor systemd passes, by convention:
// 0, 1 and 2 are the standard streams.
const listenFdsStart = 3

// socketActivatedListeners returns the listeners systemd passed, or nil if the
// process was not socket-activated.
//
// LISTEN_PID guards against inheriting the variables through a fork or an exec
// into a child, where the descriptors would belong to a different process.
func socketActivatedListeners() ([]net.Listener, error) {
	pid, err := strconv.Atoi(os.Getenv("LISTEN_PID"))
	if err != nil || pid != os.Getpid() {
		return nil, nil
	}

	n, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || n <= 0 {
		return nil, nil
	}

	// Clear them so a child process does not inherit descriptors it does not
	// own -- the same reason LISTEN_PID exists.
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")

	listeners := make([]net.Listener, 0, n)
	for i := 0; i < n; i++ {
		fd := listenFdsStart + i

		f := os.NewFile(uintptr(fd), "systemd-socket-"+strconv.Itoa(fd))
		if f == nil {
			return nil, errors.New("socket activation: LISTEN_FDS names a descriptor that is not open")
		}

		l, err := net.FileListener(f)
		// FileListener dups the descriptor, so the original is ours to close
		// either way.
		f.Close()
		if err != nil {
			return nil, err
		}

		listeners = append(listeners, l)
	}

	return listeners, nil
}

// notifyReady tells systemd the process is serving.
//
// Type=notify units stay in "activating" until this arrives, which is what
// makes `systemctl restart` block until the new binary is actually accepting
// rather than merely running. A deploy that reports success the moment the
// process starts cannot tell "started" from "working".
//
// Best effort: not being under systemd is the normal case, not an error.
func notifyReady() {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return
	}

	// A leading @ means an abstract socket, expressed to Go as a leading NUL.
	if addr[0] == '@' {
		addr = "\x00" + addr[1:]
	}

	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()

	conn.Write([]byte("READY=1"))
}
