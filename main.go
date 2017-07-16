package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/nlopes/slack"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/watch"
	"k8s.io/client-go/pkg/api"
)

// Sends a message to the Slack channel about the Event.
func sendMessage(e *api.Event, color string) error {
	api := slack.New(os.Getenv("SLACK_TOKEN"))
	params := slack.PostMessageParameters{}
	metadata := e.GetObjectMeta()
	attachment := slack.Attachment{
		// The fallback message shows in clients such as IRC or OS X notifications.
		Fallback: e.Message,
		Fields: []slack.AttachmentField{
			slack.AttachmentField{
				Title: "Env",
				Value: os.Getenv("APP_ENV"),
				Short: true,
			},
			slack.AttachmentField{
				Title: "Namespace",
				Value: metadata.GetNamespace(),
				Short: true,
			},
			slack.AttachmentField{
				Title: "Message",
				Value: e.Message,
			},
			slack.AttachmentField{
				Title: "Object",
				Value: e.InvolvedObject.Kind,
				Short: true,
			},
			slack.AttachmentField{
				Title: "Name",
				Value: metadata.GetName(),
				Short: true,
			},
			slack.AttachmentField{
				Title: "Reason",
				Value: e.Reason,
				Short: true,
			},
			slack.AttachmentField{
				Title: "Component",
				Value: e.Source.Component,
				Short: true,
			},
		},
	}

	// Use a color if provided, otherwise try to guess.
	if color != "" {
		attachment.Color = color
	} else if strings.HasPrefix(e.Reason, "Success") {
		attachment.Color = "good"
	} else if strings.HasPrefix(e.Reason, "Fail") {
		attachment.Color = "danger"
	}
	params.Attachments = []slack.Attachment{attachment}

	if strings.EqualFold(os.Getenv("EVENT_LEVEL"), "error" ) && attachment.Color == "good" {
		return nil
	}

	channelID, timestamp, err := api.PostMessage(os.Getenv("SLACK_CHANNEL"), "", params)
	if err != nil {
		fmt.Printf("%s\n", err)
		return err
	}

	log.Printf("Message successfully sent to channel %s at %s", channelID, timestamp)
	return nil
}

func main() {
	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error scanCluster must be run from inside a cluster %v", err)
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		panic(err.Error())
	}

	// Setup a watcher for events.
	eventClient := clientset.Events(api.NamespaceAll)
	w, err := eventClient.Watch(v1.ListOptions{})

	if err != nil {
		log.Fatalf("Failed to set up watch: %v", err)
	}
	for {
		select {
		case watchEvent, _ := <-w.ResultChan():

			e, _ := watchEvent.Object.(*api.Event)
			log.Printf("Reason %v", e)

			// Log all events for now.
			log.Printf("Reason: %s\nMessage: %s\nCount: %s\nFirstTimestamp: %s\nLastTimestamp: %s\n\n", e.Reason, e.Message, strconv.Itoa(int(e.Count)), e.FirstTimestamp, e.LastTimestamp)

			send := false
			color := ""
			if watchEvent.Type == watch.Added {
				send = true
				color = "good"
			} else if watchEvent.Type == watch.Deleted {
				send = true
				color = "warning"
			} else if e.Reason == "SuccessfulCreate" {
				send = true
				color = "good"
			} else if e.Reason == "NodeReady" {
				send = true
				color = "good"
			} else if e.Reason == "NodeNotReady" {
				send = true
				color = "warning"
			} else if e.Reason == "NodeOutOfDisk" {
				send = true
				color = "danger"
			}

			// kubelet and controllermanager are loud.
			if e.Source.Component == "kubelet" {
				send = false
			} else if e.Source.Component == "controllermanager" {
				send = false
			} else if e.Source.Component == "default-scheduler" {
				send = false
			}

			// For now, dont alert multiple times, except if it's a backoff
			if e.Count > 1 {
				send = false
			}
			if e.Reason == "BackOff" && e.Count == 3 {
				send = true
				color = "danger"
			}

			if send {
				sendMessage(e, color)
			}
		}
	}
}
