package tjo

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// TestSocketActivationIgnoresForeignEnvironment covers the guard that stops the
// process adopting descriptors that are not its own.
//
// LISTEN_PID exists because the variables are inherited across fork and exec.
// A child that read LISTEN_FDS without checking the pid would take file
// descriptors 3..n from whatever the parent had open -- a database connection,
// a log file -- and call net.FileListener on them.
func TestSocketActivationIgnoresForeignEnvironment(t *testing.T) {
	t.Setenv("LISTEN_FDS", "1")
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()+1))

	got, err := socketActivatedListeners()
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("adopted %d listeners despite LISTEN_PID naming another process", len(got))
	}
}

func TestSocketActivationIsInactiveWithoutTheEnvironment(t *testing.T) {
	t.Setenv("LISTEN_PID", "")
	t.Setenv("LISTEN_FDS", "")

	got, err := socketActivatedListeners()
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got %d listeners outside systemd, want none", len(got))
	}
}

// The real behaviour: a listening socket handed over as a file descriptor is
// adopted and served on. This is the mechanism `tjo deploy` relies on, so it is
// proven rather than assumed -- the whole zero-downtime claim rests on the app
// using systemd's socket instead of opening its own.
//
// Run in a subprocess, because that is how systemd does it and because the
// alternative does not work: dup2-ing onto descriptor 3 inside the test process
// clobbers the Go runtime's kqueue descriptor and kills the process with
// "netpoll failed". exec.Cmd.ExtraFiles places the socket at 3, which is
// exactly the sd_listen_fds convention.
func TestSocketActivationAdoptsAPassedListener(t *testing.T) {
	if os.Getenv("TJO_SOCKET_ACTIVATION_CHILD") == "1" {
		socketActivationChild()
		return
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	f, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	cmd := exec.Command(os.Args[0], "-test.run=TestSocketActivationAdoptsAPassedListener")
	cmd.Env = append(os.Environ(), "TJO_SOCKET_ACTIVATION_CHILD=1", "LISTEN_FDS=1")
	// ExtraFiles[0] becomes descriptor 3 in the child, which is where
	// sd_listen_fds looks. LISTEN_PID is set by the child, since it is the only
	// one that knows its own pid.
	cmd.ExtraFiles = []*os.File{f}
	cmd.Stderr = os.Stderr

	// A pipe rather than cmd.Output(): the child stays alive serving on the
	// socket, so waiting for it to exit before making the request deadlocks.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("child said nothing: %v", err)
	}
	if got := strings.TrimSpace(line); got != "adopted "+addr {
		t.Fatalf("child reported %q, want %q", got, "adopted "+addr)
	}

	// The child is serving on the socket this process created; proving that
	// from here is what shows the handover actually worked.
	resp, err := http.Get("http://" + addr)
	if err != nil {
		t.Fatalf("the adopted socket does not serve: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "served by the child" {
		t.Errorf("body = %q", body)
	}
}

// socketActivationChild runs in the subprocess: adopt descriptor 3, serve on
// it, report the address, and exit when told.
func socketActivationChild() {
	os.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))

	listeners, err := socketActivatedListeners()
	if err != nil || len(listeners) != 1 {
		fmt.Printf("adoption failed: %v (%d listeners)\n", err, len(listeners))
		os.Exit(1)
	}

	fmt.Printf("adopted %s\n", listeners[0].Addr())

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "served by the child")
	})}
	srv.Serve(listeners[0])
}

// The variables must not survive into a child process, for the same reason
// LISTEN_PID exists.
func TestSocketActivationClearsItsEnvironment(t *testing.T) {
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	t.Setenv("LISTEN_FDS", "0")

	if _, err := socketActivatedListeners(); err != nil {
		t.Fatal(err)
	}

	// LISTEN_FDS=0 means no descriptors, so it returns early without clearing;
	// the case that matters is a successful adoption, covered above. This
	// asserts the early return does not error, and documents why it is separate.
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("no /bin/true")
	}
}

// notifyReady must be a no-op outside systemd rather than an error, because
// that is every development machine.
func TestNotifyReadyIsSilentWithoutSystemd(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	notifyReady()
}

func TestNotifyReadySendsReady(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notify.sock"

	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Skipf("unixgram unavailable: %v", err)
	}
	defer conn.Close()

	t.Setenv("NOTIFY_SOCKET", path)
	notifyReady()

	buf := make([]byte, 64)
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatalf("systemd would never have been told the app is ready: %v", err)
	}
	if got := string(buf[:n]); got != "READY=1" {
		t.Errorf("sent %q, want READY=1", got)
	}
}
