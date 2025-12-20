package gemquick

type initPaths struct {
	rootPath    string
	folderNames []string
}

type cookieConfig struct {
	name     string
	lifetime string
	persist  string
	secure   string
	domain   string
}

type databaseConfig struct {
	dsn      string
	database string
}

// Database is now defined in services.go

type redisConfig struct {
	host     string
	port     string
	password string
	prefix   string
}
