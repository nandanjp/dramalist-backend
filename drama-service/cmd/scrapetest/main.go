// Scrape test tool — run to validate MDL CSS selectors against live pages.
// Usage: go run ./cmd/scrapetest
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"dramalist/drama-service/mdl"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := mdl.NewClient()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	// Test person fetch — Lee Jong Suk
	fmt.Println("=== Person: Lee Jong Suk (900-lee-jong-suk) ===")
	person, err := client.FetchPerson(ctx, "900-lee-jong-suk")
	if err != nil {
		fmt.Fprintf(os.Stderr, "person error: %v\n", err)
	} else {
		enc.Encode(person)
	}

	// Test search
	fmt.Println("\n=== Search: 'reply 1988' ===")
	results, hasMore, err := client.Search(ctx, "reply 1988", 1, 5)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search error: %v\n", err)
	} else {
		enc.Encode(map[string]any{"results": results, "has_more": hasMore})
	}

	// Test detail fetch — Wife of a 21st Century Prince
	fmt.Println("\n=== Detail: Wife of a 21st Century Prince ===")
	detail, err := client.FetchDetail(ctx, "781538-wife-of-a-21st-century-prince")
	if err != nil {
		fmt.Fprintf(os.Stderr, "detail error: %v\n", err)
		os.Exit(1)
	}
	enc.Encode(detail)
}
