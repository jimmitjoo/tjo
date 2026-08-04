package email

import (
	"testing"
	"time"
)

func TestMail_SendSMTPMessage(t *testing.T) {
	requiresMailServer(t)

	msg := Message{
		From:        "test@test.com",
		FromName:    "Test",
		To:          "to@test.com",
		Subject:     "Test",
		Template:    "test",
		Attachments: []string{"testdata/email/test.plain.tmpl"},
	}

	err := mailer.SendSMTPMessage(msg)
	if err != nil {
		t.Error(err)
	}
}

func TestMail_SendUsingChan(t *testing.T) {
	requiresMailServer(t)

	msg := Message{
		From:        "test@test.com",
		FromName:    "Test",
		To:          "to@test.com",
		Subject:     "Test",
		Template:    "test",
		Attachments: []string{"testdata/email/test.plain.tmpl"},
	}

	mailer.Jobs <- msg
	result := <-mailer.Results
	if !result.Success {
		t.Error(result.Error)
	}

	msg.To = "not_a_valid_email"
	mailer.Jobs <- msg
	result = <-mailer.Results
	if result.Success {
		t.Error("no error received with invalid To address")
	}
}

func TestMail_SendUsingAPI(t *testing.T) {
	msg := Message{
		To:          "to@test.com",
		Subject:     "Test",
		Template:    "test",
		Attachments: []string{"testdata/email/test.plain.tmpl"},
	}

	mailer.API = "non_existent_api"
	mailer.APIKey = "no_valid_api_key"
	mailer.APIUrl = "https://www.fakeurl.com"

	err := mailer.SendUsingAPI(msg, "unknown_api")
	if err == nil {
		t.Error("no error received with invalid API")
	}

	mailer.API = ""
	mailer.APIKey = ""
	mailer.APIUrl = ""
}

func TestMail_BuildHTMLMessage(t *testing.T) {
	msg := Message{
		From:        "test@test.com",
		FromName:    "Test",
		To:          "to@test.com",
		Subject:     "Test",
		Template:    "test",
		Attachments: []string{"testdata/email/test.plain.tmpl"},
	}

	_, err := mailer.buildHTMLMessage(msg)
	if err != nil {
		t.Error(err)
	}
}

func TestMail_BuildPlainTextMessage(t *testing.T) {
	msg := Message{
		From:        "test@test.com",
		FromName:    "Test",
		To:          "to@test.com",
		Subject:     "Test",
		Template:    "test",
		Attachments: []string{"testdata/email/test.plain.tmpl"},
	}

	_, err := mailer.buildPlainTextMessage(msg)
	if err != nil {
		t.Error(err)
	}
}

func TestMail_send(t *testing.T) {
	requiresMailServer(t)

	msg := Message{
		From:        "test@test.com",
		FromName:    "Test",
		To:          "to@test.com",
		Subject:     "Test",
		Template:    "test",
		Attachments: []string{"testdata/email/test.plain.tmpl"},
	}

	err := mailer.Send(msg)
	if err != nil {
		t.Error(err)
	}

	mailer.API = "non_existent_api"
	mailer.APIKey = "no_valid_api_key"
	mailer.APIUrl = "https://www.fakeurl.com"

	err = mailer.Send(msg)
	if err == nil {
		t.Error("no error received with invalid API credentials")
	}

	mailer.API = ""
	mailer.APIKey = ""
	mailer.APIUrl = ""
}

func TestMail_ChooseAPI(t *testing.T) {
	msg := Message{
		From:        "test@test.com",
		FromName:    "Test",
		To:          "to@test.com",
		Subject:     "Test",
		Template:    "test",
		Attachments: []string{"testdata/email/test.plain.tmpl"},
	}

	mailer.API = "non_existent_api"

	err := mailer.ChooseAPI(msg)
	if err == nil {
		t.Error("no error received with invalid API")
	}
}

// TestListenForMailStopsOnClose covers issue #12. A bare `msg := <-m.Jobs`
// receives the zero value from a closed channel forever, so closing Jobs --
// which is exactly how Module.Shutdown stops this goroutine -- spun at full
// tilt instead of returning. Measured over 3.4 million send attempts in four
// seconds before the fix.
func TestListenForMailStopsOnClose(t *testing.T) {
	m := &Mail{
		Templates: "./testdata/email",
		Jobs:      make(chan Message, 1),
		Results:   make(chan Result, 100),
	}

	done := make(chan struct{})
	go func() {
		m.ListenForMail()
		close(done)
	}()

	close(m.Jobs)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ListenForMail did not return after Jobs was closed")
	}

	// A spinning loop would have stuffed the Results buffer with failures for
	// the zero-value message it kept receiving.
	if n := len(m.Results); n > 0 {
		t.Errorf("ListenForMail produced %d results from a closed channel", n)
	}
}
