// Command quickstart finds a work email with the people.email.find job. PAID: it spends a
// few cents of your looot balance. Run it with LOOOT_TOKEN set.
package main

import (
	"context"
	"fmt"
	"log"

	looot "github.com/loootai/looot-go"
)

func main() {
	ctx := context.Background()
	client := looot.New() // reads LOOOT_TOKEN

	bal, err := client.Balance(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if bal.Available < 0.05 {
		fmt.Println("Add credit first:", bal.TopUpLink.URL())
		return
	}

	run, err := client.RunJob(ctx, looot.JobRunRequest{
		Job:            "people.email.find",
		Input:          map[string]any{"first_name": "Jane", "last_name": "Doe", "domain": "example.com"},
		Fallback:       looot.FallbackOn(),
		Wait:           30,
		IdempotencyKey: "quickstart-jane-doe-example-com",
	})
	if err != nil {
		log.Fatal(err)
	}
	if run.Error != nil {
		fmt.Println(run.Status, run.Error.Code, run.Error.RetryHint)
		return
	}
	fmt.Println(run.Status, run.Outcome, string(run.Result))
}
