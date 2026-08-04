package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Deployment without Docker.
//
// Kamal exists because Rails cannot be shipped as a file, so it containerises
// the app, pushes to a registry and runs a proxy on the host. A Tjo app is one
// static binary; containerising 15MB of statically linked Go to satisfy a
// deploy tool is ceremony that exists to work around a runtime we do not have.
//
// So: build, copy, restart. Nothing runs on the host but the app and, if you
// want TLS, Caddy. No registry, no orchestrator, no agent.
//
// Zero downtime comes from systemd socket activation. systemd owns the
// listening socket and passes it to the process via LISTEN_FDS, so the socket
// outlives every restart and connections queue in the kernel backlog during the
// swap. That is the whole mechanism -- no Go-side code, no proxy required. The
// graceful-restart libraries this would otherwise need have both lost:
// cloudflare/tableflip has not been touched since 2024 and jpillora/overseer
// has never tagged a release.

// deployConfig is read from deploy.conf in the project root.
//
// A flat key=value file rather than TOML or YAML: it holds six settings, and
// adding a parser dependency to read six settings is how a deploy tool starts
// growing into a product.
type deployConfig struct {
	Host    string // user@host or host
	AppName string // systemd unit and directory name
	Domain  string // optional; enables Caddy and TLS
	Port    int    // port the app listens on behind Caddy
	Path    string // install root on the host
	GOARCH  string // target architecture
}

func defaultDeployConfig() deployConfig {
	return deployConfig{Port: 8080, Path: "/opt", GOARCH: "amd64"}
}

func loadDeployConfig(root string) (deployConfig, error) {
	cfg := defaultDeployConfig()

	f, err := os.Open(filepath.Join(root, "deploy.conf"))
	if err != nil {
		return cfg, fmt.Errorf("no deploy.conf in %s -- run `tjo deploy init` first", root)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)

		switch k {
		case "HOST":
			cfg.Host = v
		case "APP_NAME":
			cfg.AppName = v
		case "DOMAIN":
			cfg.Domain = v
		case "PORT":
			cfg.Port, _ = strconv.Atoi(v)
		case "PATH":
			cfg.Path = v
		case "GOARCH":
			cfg.GOARCH = v
		}
	}
	if err := sc.Err(); err != nil {
		return cfg, err
	}

	if cfg.Host == "" {
		return cfg, fmt.Errorf("deploy.conf: HOST is required")
	}
	if cfg.AppName == "" {
		return cfg, fmt.Errorf("deploy.conf: APP_NAME is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}

	return cfg, nil
}

func doDeployInit(root string) error {
	path := filepath.Join(root, "deploy.conf")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	name := filepath.Base(root)
	content := fmt.Sprintf(`# Where and what to deploy. Six settings, no parser.
#
# tjo deploy builds a static binary, copies it over SSH, and restarts a systemd
# unit that owns its listening socket -- so restarts drop no connections. There
# is no Docker, no registry and nothing running on the host but your app and,
# if DOMAIN is set, Caddy for TLS.

HOST=root@example.com
APP_NAME=%s

# Optional. Set it and Caddy is installed and configured to terminate TLS and
# proxy to PORT. Leave it empty to serve directly, or to terminate elsewhere.
DOMAIN=

PORT=8080
PATH=/opt

# The target's architecture. Hetzner and most VPS providers are amd64; ARM
# instances are arm64.
GOARCH=amd64
`, name)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Println("Created deploy.conf. Edit HOST and APP_NAME, then run `tjo deploy`.")
	return nil
}

const systemdUnit = `[Unit]
Description=%[1]s
After=network.target
Requires=%[1]s.socket

[Service]
Type=notify
NotifyAccess=all
WorkingDirectory=%[2]s/%[1]s/current
ExecStart=%[2]s/%[1]s/current/%[1]s
Restart=on-failure
RestartSec=1

# The socket is passed in by systemd rather than opened by the process, so it
# survives restarts and connections queue in the kernel backlog during a swap.
# This is what makes the deploy below drop nothing.
StandardOutput=journal
StandardError=journal

NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=full
ProtectHome=yes

[Install]
WantedBy=multi-user.target
`

const systemdSocket = `[Unit]
Description=%[1]s socket

[Socket]
ListenStream=%[3]d
# Keep the socket open across restarts of the service; this is the property the
# whole zero-downtime story rests on.
Accept=no

[Install]
WantedBy=sockets.target
`

const caddyfile = `%[1]s {
	reverse_proxy 127.0.0.1:%[2]d
}
`

func doDeploy(root string) error {
	cfg, err := loadDeployConfig(root)
	if err != nil {
		return err
	}

	release := time.Now().UTC().Format("20060102150405")
	base := fmt.Sprintf("%s/%s", cfg.Path, cfg.AppName)
	dir := fmt.Sprintf("%s/releases/%s", base, release)

	fmt.Printf("Deploying %s to %s\n\n", cfg.AppName, cfg.Host)

	// 1. Build. CGO_ENABLED=0 is why this cross-compiles at all: with a cgo
	// SQLite driver, building a Linux binary from a developer's Mac is
	// impossible, which is what #47 was for.
	fmt.Printf("  building linux/%s ... ", cfg.GOARCH)
	binary := filepath.Join(os.TempDir(), cfg.AppName)
	build := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", binary, ".")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+cfg.GOARCH)
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Println("failed")
		return fmt.Errorf("build: %w\n%s", err, out)
	}
	info, err := os.Stat(binary)
	if err != nil {
		return err
	}
	fmt.Printf("%.1f MB\n", float64(info.Size())/(1<<20))

	// 2. Provision. Idempotent, so the first deploy and the hundredth run the
	// same path and there is no separate "setup" command to forget.
	fmt.Print("  provisioning host      ... ")
	unit := fmt.Sprintf(systemdUnit, cfg.AppName, cfg.Path)
	socket := fmt.Sprintf(systemdSocket, cfg.AppName, cfg.Path, cfg.Port)

	provision := fmt.Sprintf(`set -e
mkdir -p %[1]s/releases %[1]s/shared
cat > /etc/systemd/system/%[2]s.service <<'UNIT'
%[3]s
UNIT
cat > /etc/systemd/system/%[2]s.socket <<'SOCKET'
%[4]s
SOCKET
systemctl daemon-reload
systemctl enable --now %[2]s.socket
mkdir -p %[5]s`, base, cfg.AppName, unit, socket, dir)

	if err := ssh(cfg.Host, provision); err != nil {
		fmt.Println("failed")
		return err
	}
	fmt.Println("ok")

	// 3. Copy the binary and whatever it needs beside it.
	fmt.Print("  copying                ... ")
	if err := scp(binary, fmt.Sprintf("%s:%s/%s", cfg.Host, dir, cfg.AppName)); err != nil {
		fmt.Println("failed")
		return err
	}
	for _, extra := range []string{".env", "views", "public", "migrations"} {
		src := filepath.Join(root, extra)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := scpRecursive(src, fmt.Sprintf("%s:%s/", cfg.Host, dir)); err != nil {
			fmt.Println("failed")
			return err
		}
	}
	fmt.Println("ok")

	// 4. Swap and restart. The previous release is remembered before the
	// symlink moves, so a failed health check has somewhere to go back to.
	fmt.Print("  activating             ... ")
	activate := fmt.Sprintf(`set -e
chmod +x %[1]s/%[2]s
PREVIOUS=$(readlink %[3]s/current || true)
echo "$PREVIOUS" > %[3]s/previous
ln -sfn %[1]s %[3]s/current
systemctl restart %[2]s.service`, dir, cfg.AppName, base)

	if err := ssh(cfg.Host, activate); err != nil {
		fmt.Println("failed")
		return err
	}
	fmt.Println("ok")

	// 5. Health check, and roll back if it fails. Restarting successfully is
	// not the same as working, and a deploy tool that cannot tell the
	// difference is a deploy tool that ships outages.
	fmt.Print("  health check           ... ")
	health := fmt.Sprintf(`for i in $(seq 1 30); do
  if curl -sf -o /dev/null http://127.0.0.1:%d/health; then exit 0; fi
  sleep 1
done
exit 1`, cfg.Port)

	if err := ssh(cfg.Host, health); err != nil {
		fmt.Println("failed, rolling back")

		rollback := fmt.Sprintf(`set -e
PREVIOUS=$(cat %[1]s/previous)
if [ -n "$PREVIOUS" ]; then
  ln -sfn "$PREVIOUS" %[1]s/current
  systemctl restart %[2]s.service
  echo "rolled back to $PREVIOUS"
else
  echo "no previous release to roll back to"
fi`, base, cfg.AppName)

		if rbErr := ssh(cfg.Host, rollback); rbErr != nil {
			return fmt.Errorf("health check failed and rollback also failed: %w", rbErr)
		}
		return fmt.Errorf("health check failed; rolled back")
	}
	fmt.Println("ok")

	// 6. TLS, if asked for.
	if cfg.Domain != "" {
		fmt.Print("  caddy                  ... ")
		caddy := fmt.Sprintf(`set -e
if ! command -v caddy >/dev/null; then
  apt-get update -qq
  apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -qq
  apt-get install -y -qq caddy
fi
cat > /etc/caddy/Caddyfile <<'CADDY'
%s
CADDY
systemctl reload caddy || systemctl restart caddy`, fmt.Sprintf(caddyfile, cfg.Domain, cfg.Port))

		if err := ssh(cfg.Host, caddy); err != nil {
			fmt.Println("failed")
			return err
		}
		fmt.Println("ok")
	}

	// 7. Keep the last five releases. Unbounded release directories fill a
	// disk slowly enough that it happens at the worst time.
	_ = ssh(cfg.Host, fmt.Sprintf(
		`ls -1dt %s/releases/* 2>/dev/null | tail -n +6 | xargs -r rm -rf`, base))

	fmt.Printf("\nDeployed %s.\n", release)
	if cfg.Domain != "" {
		fmt.Printf("https://%s\n", cfg.Domain)
	}
	return nil
}

// ssh runs a script on the host.
//
// Shelling out to the user's ssh rather than using golang.org/x/crypto/ssh:
// every target already has it, it honours ~/.ssh/config, agent forwarding,
// jump hosts and hardware keys, and it is a dependency this does not add.
func ssh(host, script string) error {
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", host, "bash -s")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func scp(src, dst string) error {
	cmd := exec.Command("scp", "-q", "-o", "BatchMode=yes", src, dst)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func scpRecursive(src, dst string) error {
	cmd := exec.Command("scp", "-rq", "-o", "BatchMode=yes", src, dst)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
