package email

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"text/template"
	"time"

	apimail "github.com/ainsleyclark/go-mail"
	"github.com/vanng822/go-premailer/premailer"
	mail "github.com/xhit/go-simple-mail/v2"
)

type Mail struct {
	Domain     string
	Templates  string
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string
	From       string
	FromName   string
	Jobs       chan Message
	Results    chan Result
	API        string
	APIKey     string
	APIUrl     string
}

type Message struct {
	From        string
	FromName    string
	To          string
	Subject     string
	Template    string
	Attachments []string
	Data        interface{}
}

type Result struct {
	Success bool
	Error   error
}

// ListenForMail sends queued messages until Jobs is closed.
//
// The loop ranges rather than doing a bare `msg := <-m.Jobs`. A closed channel
// yields the zero Message immediately and forever, so the single-value receive
// turned Module.Shutdown -- which closes Jobs precisely to stop this goroutine
// -- into a hot spin that tried to send the empty message millions of times a
// second instead of returning.
func (m *Mail) ListenForMail() {
	for msg := range m.Jobs {
		err := m.Send(msg)
		if err != nil {
			m.Results <- Result{Success: false, Error: err}
		} else {
			m.Results <- Result{Success: true}
		}
	}
}

func (m *Mail) Send(msg Message) error {
	var err error
	if m.API != "" && m.APIKey != "" && m.APIUrl != "" && m.API != "smtp" {
		// TODO: err = m.SendAPI(msg)
		return m.ChooseAPI(msg)
	} else {
		err = m.SendSMTPMessage(msg)
	}
	return err
}

func (m *Mail) ChooseAPI(msg Message) error {
	switch m.API {
	case "mailgun", "sparkpost", "sendgrid":
		return m.SendUsingAPI(msg, m.API)
	default:
		return fmt.Errorf("API %s is not supported", m.API)
	}
}

func (m *Mail) SendUsingAPI(msg Message, transport string) error {
	if msg.From == "" {
		msg.From = m.From
	}
	if msg.FromName == "" {
		msg.FromName = m.FromName
	}

	cfg := apimail.Config{
		URL:         m.APIUrl,
		APIKey:      m.APIKey,
		Domain:      m.Domain,
		FromAddress: msg.From,
		FromName:    msg.FromName,
	}

	driver, err := apimail.NewClient(transport, cfg)
	if err != nil {
		return err
	}

	formattedMessage, err := m.buildHTMLMessage(msg)
	if err != nil {
		return err
	}
	plainTextMessage, err := m.buildPlainTextMessage(msg)
	if err != nil {
		return err
	}

	tx := &apimail.Transmission{
		Recipients: []string{msg.To},
		Subject:    msg.Subject,
		HTML:       formattedMessage,
		PlainText:  plainTextMessage,
	}

	// add attachments
	err = m.addAPIAttachments(msg, *tx)
	if err != nil {
		return err
	}

	_, err = driver.Send(tx)
	if err != nil {
		return err
	}

	return nil
}

func (m *Mail) addAPIAttachments(msg Message, tx apimail.Transmission) error {
	if len(msg.Attachments) > 0 {
		var attachments []apimail.Attachment

		for _, attachment := range msg.Attachments {
			var attach apimail.Attachment
			content, err := ioutil.ReadFile(attachment)
			if err != nil {
				return err
			}

			fileName := filepath.Base(attachment)
			attach.Bytes = content
			attach.Filename = fileName
			attachments = append(attachments, attach)

		}

		tx.Attachments = attachments
	}

	return nil
}

func (m *Mail) SendSMTPMessage(msg Message) error {

	formattedMessage, err := m.buildHTMLMessage(msg)
	if err != nil {
		return err
	}

	plainTextMessage, err := m.buildPlainTextMessage(msg)
	if err != nil {
		return err
	}

	server := mail.NewSMTPClient()
	server.Host = m.Host
	server.Port = m.Port
	server.Username = m.Username
	server.Password = m.Password
	server.Encryption = m.getEncryption(m.Encryption)
	server.KeepAlive = false
	server.ConnectTimeout = 10 * time.Second
	server.SendTimeout = 10 * time.Second

	smtpClient, err := server.Connect()
	if err != nil {
		return err
	}

	email := mail.NewMSG()
	email.SetFrom(msg.From).AddTo(msg.To).SetSubject(msg.Subject)
	email.SetBody(mail.TextHTML, formattedMessage)
	email.AddAlternative(mail.TextPlain, plainTextMessage)

	if len(msg.Attachments) > 0 {
		for _, attachment := range msg.Attachments {
			email.AddAttachment(attachment)
		}
	}

	err = email.Send(smtpClient)
	if err != nil {
		return err
	}

	return nil
}

func (m *Mail) getEncryption(encryption string) mail.Encryption {
	switch encryption {
	case "tls":
		return mail.EncryptionSTARTTLS
	case "ssl":
		return mail.EncryptionSSL
	case "none":
		return mail.EncryptionNone
	default:
		return mail.EncryptionSTARTTLS
	}
}

func (m *Mail) buildHTMLMessage(msg Message) (string, error) {

	templateToRender := fmt.Sprintf("%s/%s.html.tmpl", m.Templates, msg.Template)

	t, err := template.New("email-html").ParseFiles(templateToRender)
	if err != nil {
		return "", err
	}

	var htmlMessage bytes.Buffer
	if err = t.ExecuteTemplate(&htmlMessage, "body", msg.Data); err != nil {
		return "", err
	}

	formattedMessage := htmlMessage.String()
	formattedMessage, err = m.inlineCSS(formattedMessage)
	if err != nil {
		return "", err
	}

	return formattedMessage, nil
}

func (m *Mail) buildPlainTextMessage(msg Message) (string, error) {
	templateToRender := fmt.Sprintf("%s/%s.plain.tmpl", m.Templates, msg.Template)

	t, err := template.New("email-html").ParseFiles(templateToRender)
	if err != nil {
		return "", err
	}

	var htmlMessage bytes.Buffer
	if err = t.ExecuteTemplate(&htmlMessage, "body", msg.Data); err != nil {
		return "", err
	}

	formattedMessage := htmlMessage.String()

	return formattedMessage, nil
}

func (m *Mail) inlineCSS(s string) (string, error) {
	options := premailer.Options{
		RemoveClasses:     false,
		CssToAttributes:   false,
		KeepBangImportant: true,
	}

	prem, err := premailer.NewPremailerFromString(s, &options)
	if err != nil {
		return "", err
	}

	s, err = prem.Transform()
	if err != nil {
		return "", err
	}

	return s, nil
}
