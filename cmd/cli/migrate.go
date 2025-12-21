package main

import "github.com/jimmitjoo/tjo/core"

func doMigrate(arg2, arg3 string) error {
	dsn := getDSN()
	rootPath := getRootPath()

	switch arg2 {
	case "up":
		err := core.MigrateUp(rootPath, dsn)
		if err != nil {
			return err
		}
	case "down":
		if arg3 == "all" {
			err := core.MigrateDownAll(rootPath, dsn)
			if err != nil {
				return err
			}
			return nil
		} else {
			err := core.MigrateSteps(-1, rootPath, dsn)
			if err != nil {
				return err
			}
			return nil
		}
	case "reset":
		err := core.MigrateDownAll(rootPath, dsn)
		if err != nil {
			return err
		}
		err = core.MigrateUp(rootPath, dsn)
		if err != nil {
			return err
		}

	default:
		showHelp()
	}
	return nil
}
