package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/fsouza/go-dockerclient"
)

func healtcheck(client *docker.Client, container *docker.Container) error {
	cmd := []string{"/bin/sh", "-c"}
	for _, value := range container.Config.Healthcheck.Test[1:len(container.Config.Healthcheck.Test)] {
		cmd = append(cmd, value)
	}
	exec, err := client.CreateExec(docker.CreateExecOptions{
		AttachStderr: true,
		AttachStdin:  true,
		AttachStdout: true,
		Tty:          false,
		Cmd:          cmd,
		Container:    container.ID,
	})
	if err != nil {
		return err
	}

	var stdout, stderr bytes.Buffer
	var reader = strings.NewReader("")

	err = client.StartExec(exec.ID, docker.StartExecOptions{
		OutputStream: &stdout,
		ErrorStream:  &stderr,
		InputStream:  reader,
		RawTerminal:  true,
	})
	if err != nil {
		return err
	}

	fmt.Printf("stdout: '%s'\n", stdout.String())
	return nil
}

func main() {
	endpoint := flag.String("endpoint", "unix:///var/run/docker.sock", "Docker socket")

	client, err := docker.NewClient(*endpoint)
	if err != nil {
		fmt.Printf("Error (%s) %s\n", *endpoint, err.Error())
		os.Exit(1)
	}

	events := make(chan *docker.APIEvents)
	client.AddEventListener(events)
	fmt.Println("Listening...")

	for {
		select {
		case event := <-events:
			switch event.Status {
			case "start":
				fmt.Printf("> START %s: %s\n", event.From, event.ID)

				container, err := client.InspectContainer(event.ID)
				if err != nil {
					fmt.Printf("  %s\n", err.Error())
					break
				}

				fmt.Printf("  %+v\n", container.Config.Healthcheck)
				if container.Config.Healthcheck == nil || len(container.Config.Healthcheck.Test) == 0 {
					fmt.Printf("  No Healthcheck, assuming container started.")
					break
				}

				healtcheck(client, container)

			case "stop":
				fmt.Printf("> STOP %s: %s\n", event.From, event.ID)
			case "die":
				fmt.Printf("> DIE %s: %s\n", event.From, event.ID)
			}
		}
	}
}