package main

import (
	"fmt"
	"os"

	searchindex "github.com/my-search-index/search-index"
)

const indexPath = "search.idx"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "add":
		arg, ok := commandArg()
		if !ok {
			printUsage()
			os.Exit(1)
		}
		if err := addPath(arg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "remove":
		arg, ok := commandArg()
		if !ok {
			printUsage()
			os.Exit(1)
		}
		if err := removePath(arg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "list":
		if err := listDocuments(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "search":
		arg, ok := commandArg()
		if !ok {
			printUsage()
			os.Exit(1)
		}
		if err := search(arg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(1)
	}

}

func commandArg() (string, bool) {
	if len(os.Args) < 3 {
		return "", false
	}
	return os.Args[2], true
}

func addPath(path string) error {
	idx, err := searchindex.Load(indexPath)
	if err != nil {
		return fmt.Errorf("loading index: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("reading path: %w", err)
	}

	if info.IsDir() {
		extensions := []string{".txt", ".md"}
		if err := idx.AddDocuments(path, extensions); err != nil {
			return fmt.Errorf("indexing directory %s: %w", path, err)
		}
		fmt.Printf("indexed directory: %s\n", path)
	} else {
		if err := idx.AddDocument(path); err != nil {
			return fmt.Errorf("indexing %s: %w", path, err)
		}
		doc := idx.Docs[idx.NextDocID-1]
		fmt.Printf("indexed: %s (%d tokens)\n", path, doc.Length)
	}

	return idx.Save(indexPath)
}

func removePath(path string) error {
	idx, err := searchindex.Load(indexPath)
	if err != nil {
		return fmt.Errorf("loading index: %w", err)
	}

	if err := idx.RemoveDocument(path); err != nil {
		return err
	}

	if err := idx.Save(indexPath); err != nil {
		return err
	}

	fmt.Printf("removed: %s\n", path)
	return nil
}

func listDocuments() error {
	idx, err := searchindex.Load(indexPath)
	if err != nil {
		return fmt.Errorf("loading index: %w", err)
	}

	documents := idx.ListDocuments()
	fmt.Printf("%d documents indexed:\n", len(documents))
	for _, doc := range documents {
		fmt.Printf("  %d. %s (%d tokens)\n", doc.ID, doc.FilePath, doc.Length)
	}
	return nil
}

func search(query string) error {
	idx, err := searchindex.Load(indexPath)
	if err != nil {
		return fmt.Errorf("loading index: %w", err)
	}

	results := idx.Search(query)
	if len(results) == 0 {
		fmt.Printf("no results found for %q\n", query)
		return nil
	}

	fmt.Printf("Results for %q:\n", query)
	for i, result := range results {
		fmt.Printf(" %d. %s (score: %.4f)\n", i+1, result.Doc.FilePath, result.Score)

		for _, snippet := range result.Snippets {
			fmt.Printf("    ...%s...\n", snippet.Text)
		}
	}
	return nil
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println(" go run ./cmd/searchcli add <filepath.extension>")
	fmt.Println(" go run ./cmd/searchcli add <directory>")
	fmt.Println(" go run ./cmd/searchcli remove <filepath.extension>")
	fmt.Println(" go run ./cmd/searchcli list")
	fmt.Println(" go run ./cmd/searchcli search <query>")
}
