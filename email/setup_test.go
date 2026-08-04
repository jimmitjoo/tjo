package email

import (
	"flag"
	"log"
	"os"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

var pool *dockertest.Pool
var resource *dockertest.Resource

var mailer = Mail{
	Domain:     "localhost",
	Templates:  "./testdata/email",
	Host:       "localhost",
	Port:       1026,
	Encryption: "none",
	From:       "test@localhost.com",
	FromName:   "Test",
	Jobs:       make(chan Message, 1),
	Results:    make(chan Result, 1),
}

func TestMain(m *testing.M) {
	// Honour -short like the rest of the repo does. This module is not covered
	// by `go test ./...` from the root (it is a separate module in go.work), so
	// nobody noticed it started a container regardless. It also binds host port
	// 1026 unconditionally and log.Fatals if anything else already has it.
	flag.Parse()
	if testing.Short() {
		log.Println("skipping email tests in short mode: they require Docker")
		os.Exit(0)
	}

	// Check if Docker is available
	var err error
	pool, err = dockertest.NewPool(os.Getenv("DOCKER_HOST"))
	if err != nil {
		log.Println("Docker not available, skipping email tests:", err)
		// Still exit with 0 to not fail the test suite
		os.Exit(0)
	}

	opts := dockertest.RunOptions{
		Repository:   "mailhog/mailhog",
		Tag:          "latest",
		Env:          []string{},
		ExposedPorts: []string{"1025", "8025"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"1025": {{HostIP: "0.0.0.0", HostPort: "1026"}},
			"8025": {{HostIP: "0.0.0.0", HostPort: "8026"}},
		},
	}

	resource, err = pool.RunWithOptions(&opts)

	if err != nil {
		log.Println(err)
		if resource != nil {
			if errPurge := pool.Purge(resource); errPurge != nil {
				log.Println("could not purge resource", errPurge)
			}
		}
		log.Fatal("could not start resource", err)
	}

	time.Sleep(2 * time.Second)

	go mailer.ListenForMail()

	code := m.Run()

	if err := pool.Purge(resource); err != nil {
		log.Fatal("could not purge resource", err)
	}

	os.Exit(code)
}
