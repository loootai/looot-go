package looot_test

import (
	"context"
	"errors"
	"fmt"

	looot "github.com/loootai/looot-go"
)

// Check the balance, then find a work email. looot picks the provider and falls back to the
// next one on a miss.
func Example() {
	ctx := context.Background()
	client := looot.New() // reads LOOOT_TOKEN

	bal, err := client.Balance(ctx)
	if err != nil {
		panic(err)
	}
	if bal.Available < 0.05 {
		fmt.Println("Add credit first:", bal.TopUpLink.URL())
		return
	}
	run, err := client.RunJob(ctx, looot.JobRunRequest{
		Job:      "people.email.find",
		Input:    map[string]any{"first_name": "Jane", "last_name": "Doe", "domain": "example.com"},
		Fallback: looot.FallbackOn(),
		Wait:     30,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(run.Status, run.Outcome)
}

// HTTP errors are *looot.Error. A 402 carries the top-up link.
func ExampleError() {
	_, err := looot.New().Run(context.Background(), looot.RunRequest{EndpointID: "icypeas-email-verify"})
	var le *looot.Error
	if errors.As(err, &le) {
		switch {
		case le.Code == "insufficient_balance" && le.TopUp != nil:
			fmt.Println("Add credit:", le.TopUp.URL())
		case le.Retryable:
			fmt.Println("Try again later:", le.RetryHint)
		default:
			fmt.Println(le.Code, le.Message, le.RequestID)
		}
	}
}
