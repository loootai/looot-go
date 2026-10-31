// Command overview lists the jobs that match a topic. Free, no token needed.
package main

import (
	"context"
	"fmt"
	"log"

	looot "github.com/loootai/looot-go"
)

func main() {
	overview, err := looot.New().CatalogOverview(context.Background(), &looot.CatalogOverviewParams{Topic: "email"})
	if err != nil {
		log.Fatal(err)
	}
	for _, job := range overview.Jobs {
		fmt.Printf("%-28s providers=%d\n", job.ID, job.ProviderCount)
	}
}
