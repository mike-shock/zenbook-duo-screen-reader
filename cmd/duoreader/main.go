package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"

	"duoreader/internal/app"
	"duoreader/internal/platform"
)

func main() {
	books := flag.String("books", "", "каталог библиотеки (по умолчанию books рядом с exe)")
	data := flag.String("data", "", "каталог настроек и импортированных книг")
	serve := flag.Bool("serve", false, "только локальный сервер для браузера/диагностики")
	addr := flag.String("listen", "127.0.0.1:0", "адрес локального сервера")
	flag.Parse()
	exe, _ := os.Executable()
	if *books == "" {
		for _, candidate := range []string{filepath.Join(filepath.Dir(exe), "books"), filepath.Join(filepath.Dir(exe), "..", "books"), "books"} {
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				*books = candidate
				break
			}
		}
		if *books == "" {
			*books = "books"
		}
	}
	if *data == "" {
		base, _ := os.UserConfigDir()
		if runtime.GOOS == "windows" && os.Getenv("LOCALAPPDATA") != "" {
			base = os.Getenv("LOCALAPPDATA")
		}
		*data = filepath.Join(base, "DuoReader")
	}
	if err := os.MkdirAll(*data, 0700); err == nil {
		if logfile, err := os.OpenFile(filepath.Join(*data, "reader.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600); err == nil {
			defer logfile.Close()
			log.SetOutput(io.MultiWriter(os.Stderr, logfile))
		}
	}
	s, err := app.New(*books, *data)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	if flag.NArg() > 0 {
		if err := s.OpenPath(flag.Arg(0)); err != nil {
			log.Print(err)
		}
	} else {
		s.Restore()
	}
	url, err := s.Start(*addr)
	if err != nil {
		log.Fatal(err)
	}
	if *serve {
		fmt.Println(url)
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		return
	}
	if err := platform.Run(url, *data, s); err != nil {
		log.Print(err)
		fmt.Println("Откройте в браузере:", url)
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
	}
}
