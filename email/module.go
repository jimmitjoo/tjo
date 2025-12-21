package email

import (
	"context"
	"os"
	"strconv"
)

// Module implements the gemquick.Module interface for email functionality.
// Use this to opt-in to email support in your application.
//
// Example:
//
//	app := gemquick.Gemquick{}
//	app.New(rootPath, email.NewModule())
//
//	// Later, send email:
//	if emailModule := app.Modules.Get("email"); emailModule != nil {
//	    m := emailModule.(*email.Module)
//	    m.Send(email.Message{To: "user@example.com", Subject: "Hello"})
//	}
type Module struct {
	Mail   *Mail
	config *Config
}

// Config holds email module configuration
type Config struct {
	Templates string

	// SMTP settings
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string

	// Sender defaults
	Domain   string
	From     string
	FromName string

	// API provider settings (alternative to SMTP)
	API    string
	APIKey string
	APIURL string
}

// Option is a function that configures the email module
type Option func(*Module)

// NewModule creates a new email module with default configuration from environment.
func NewModule(opts ...Option) *Module {
	port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))

	m := &Module{
		config: &Config{
			Templates:  os.Getenv("EMAIL_TEMPLATES"),
			Host:       os.Getenv("SMTP_HOST"),
			Port:       port,
			Username:   os.Getenv("SMTP_USERNAME"),
			Password:   os.Getenv("SMTP_PASSWORD"),
			Encryption: os.Getenv("SMTP_ENCRYPTION"),
			Domain:     os.Getenv("MAIL_DOMAIN"),
			From:       os.Getenv("MAIL_FROM_ADDRESS"),
			FromName:   os.Getenv("MAIL_FROM_NAME"),
			API:        os.Getenv("MAIL_API"),
			APIKey:     os.Getenv("MAIL_API_KEY"),
			APIURL:     os.Getenv("MAIL_API_URL"),
		},
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// WithConfig sets custom configuration for the email module
func WithConfig(cfg *Config) Option {
	return func(m *Module) {
		m.config = cfg
	}
}

// WithSMTP configures SMTP settings
func WithSMTP(host string, port int, username, password, encryption string) Option {
	return func(m *Module) {
		m.config.Host = host
		m.config.Port = port
		m.config.Username = username
		m.config.Password = password
		m.config.Encryption = encryption
	}
}

// WithFrom sets the default sender
func WithFrom(email, name string) Option {
	return func(m *Module) {
		m.config.From = email
		m.config.FromName = name
	}
}

// WithTemplates sets the email templates directory
func WithTemplates(path string) Option {
	return func(m *Module) {
		m.config.Templates = path
	}
}

// WithAPI configures an API-based email provider (mailgun, sendgrid, sparkpost)
func WithAPI(provider, apiKey, apiURL, domain string) Option {
	return func(m *Module) {
		m.config.API = provider
		m.config.APIKey = apiKey
		m.config.APIURL = apiURL
		m.config.Domain = domain
	}
}

// Name returns the module identifier
func (m *Module) Name() string {
	return "email"
}

// Initialize sets up the email service.
// This is called automatically during app.New().
func (m *Module) Initialize(g interface{}) error {
	m.Mail = &Mail{
		Templates:  m.config.Templates,
		Host:       m.config.Host,
		Port:       m.config.Port,
		Username:   m.config.Username,
		Password:   m.config.Password,
		Encryption: m.config.Encryption,
		Domain:     m.config.Domain,
		From:       m.config.From,
		FromName:   m.config.FromName,
		API:        m.config.API,
		APIKey:     m.config.APIKey,
		APIUrl:     m.config.APIURL,
		Jobs:       make(chan Message, 20),
		Results:    make(chan Result, 20),
	}

	// Start the mail listener goroutine
	go m.Mail.ListenForMail()

	return nil
}

// Shutdown gracefully stops the email module.
// Closes the Jobs channel to stop the listener goroutine.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.Mail != nil && m.Mail.Jobs != nil {
		close(m.Mail.Jobs)
	}
	return nil
}

// Send sends an email message using the configured provider.
func (m *Module) Send(msg Message) error {
	return m.Mail.Send(msg)
}

// Queue adds an email message to the job queue for async sending.
func (m *Module) Queue(msg Message) {
	if m.Mail != nil && m.Mail.Jobs != nil {
		m.Mail.Jobs <- msg
	}
}

// IsConfigured returns true if email is properly configured
func (m *Module) IsConfigured() bool {
	if m.Mail == nil {
		return false
	}
	// Either SMTP or API must be configured
	hasSMTP := m.Mail.Host != "" && m.Mail.Port > 0
	hasAPI := m.Mail.API != "" && m.Mail.APIKey != ""
	return hasSMTP || hasAPI
}
