package app

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"reflect"
	"strings"

	"github.com/go-pkgz/lgr"
	"github.com/umputun/go-flags"
)

type Debug struct {
	Debug bool `long:"debug" env:"DEBUG" description:"enable debug mode"`
}

func (d Debug) debugEnabled() bool {
	return d.Debug
}

type debug interface {
	debugEnabled() bool
}

func LoadConfiguration(title, revision string, opts any) (lgr.L, error) {
	err := ParseConfiguration(opts)
	if err != nil {
		return nil, err
	}

	log := lgr.New(lgr.Msec, lgr.LevelBraces)

	if d, ok := opts.(debug); ok {
		if d.debugEnabled() {
			log = lgr.New(lgr.Debug, lgr.CallerFile, lgr.CallerFunc, lgr.Msec, lgr.LevelBraces)
		}
	}

	printConfiguration(log, title, revision, opts)
	return log, nil
}

func ParseConfiguration(opts any) error {
	p := flags.NewParser(opts, flags.PrintErrors|flags.PassDoubleDash|flags.HelpFlag|flags.IgnoreUnknown)
	p.SubcommandsOptional = true

	if _, err := p.Parse(); err != nil {
		if err.(*flags.Error).Type != flags.ErrHelp {
			log.Printf("[ERROR] cli error: %v", err)
		}
		return err
	}

	return nil
}

func printConfiguration(log lgr.L, title string, revision string, opts any) {
	log.Logf("[INFO] Application: %v (rev: %v)", title, revision)

	buf := new(bytes.Buffer)
	structPrinter(buf, 0, "", opts)
	log.Logf("[DEBUG] \n%s", buf.String())
}

func structPrinter(b io.Writer, lvl int, name string, v any) {
	val := reflect.ValueOf(v)

	switch val.Kind() {
	case reflect.Ptr:
		val = val.Elem()
		structPrinter(b, lvl, "", val.Interface())
		return
	case reflect.Struct:
		for i := 0; i < val.NumField(); i++ {
			field := val.Field(i)
			val := val.Type().Field(i).Name

			if field.Kind() == reflect.Ptr {
				if field.IsZero() {
					printSingleValue(b, lvl+1, val, "nil")
					continue
				}

				field = field.Elem()
			}

			if field.Kind() == reflect.Struct {
				printSingleValue(b, lvl+1, val, "")
			}

			structPrinter(b, lvl+1, val, field.Interface())
		}
	default:
		printSingleValue(b, lvl, name, val.Interface())
	}
}

func printSingleValue(b io.Writer, tab int, name string, val any) {
	for i := 0; i < tab; i++ {
		b.Write([]byte("  "))
	}

	switch strings.ToLower(name) {
	case "password", "pass", "token", "secret":
		val = "********"
	}

	b.Write([]byte(fmt.Sprintf("%s:  %v\n", name, val)))
}
