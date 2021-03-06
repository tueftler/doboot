package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/fsouza/go-dockerclient"
	"github.com/tueftler/boot/addr"
	"github.com/tueftler/boot/command"
	"github.com/tueftler/boot/events"
	"github.com/tueftler/boot/output"
	"github.com/tueftler/boot/proxy"
)

// Intercept start event, running and waiting for boot command
func start(log *output.Stream, client *docker.Client, event *docker.APIEvents) events.Action {
	stream := log.Prefixed(output.Text("container", event.Actor.ID[0:13]+" | "))

	container, err := client.InspectContainer(event.Actor.ID)
	if err != nil {
		stream.Error("Inspect error %s", err.Error())
		return &events.Drop{}
	}

	boot := command.Boot(client, container)
	stream.Info("Using boot command %s", boot)
	result, err := boot.Run(stream)
	if err != nil {
		stream.Error("Run error %s", err.Error())
		return &events.Drop{}
	}

	switch result {
	case command.NOTRUN:
		stream.Warning("No boot command present, assuming container started")
		return &events.Emit{Event: event}

	case 0:
		stream.Success("Up and running!")
		return &events.Emit{Event: event}

	default:
		stream.Error("Non-zero exit code %d", result)
		return &events.Drop{}
	}
}

// Graceful shutdown on Ctrl+C
func wait(done chan bool) os.Signal {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)

	select {
	case sig := <-sigs:
		fmt.Printf("\r")
		done <- true
		return sig

	case <-done:
		return nil
	}
}

// Runs daemon
func run(connect, listen addr.Addr) error {
	client, err := docker.NewClient(connect.String())
	if err != nil {
		return fmt.Errorf("Connect '%s': %s", connect, err.Error())
	}

	err = client.Ping()
	if err != nil {
		return fmt.Errorf("Ping '%s': %s", connect, err.Error())
	}

	server, err := listen.Listen()
	if err != nil {
		return fmt.Errorf("Listen '%s': %s", listen, err.Error())
	}

	events := events.Distribute(client, output.NewStream(output.Text("proxy", "distribute    | "), output.Print))
	proxy := proxy.Pass(connect, output.NewStream(output.Text("proxy", "proxy         | "), output.Print))

	urls := http.NewServeMux()
	urls.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			events.ServeHTTP(w, r)
		} else {
			proxy.ServeHTTP(w, r)
		}
	})
	go http.Serve(server, urls)

	done := make(chan bool, 1)
	events.Intercept("start", start)
	events.Log.Info("Listening...")
	go events.Listen(done)

	if sig := wait(done); sig != nil {
		events.Log.Info("Received %s, shutting down", sig)
	}
	return nil
}

// Parse command line and run boot
func main() {
	docker := flag.String("docker", "unix:///var/run/docker.sock", "Docker socket")
	listen := flag.String("listen", "unix:///var/run/boot.sock", "Boot socket")
	flag.Parse()

	if err := run(addr.Flag(*docker), addr.Flag(*listen)); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
