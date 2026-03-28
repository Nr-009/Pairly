package executor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"

	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
)

type TreeNode struct {
	File     pg.File
	Children []*TreeNode
}

func ReconstructFileTree(
	ctx context.Context,
	db *pgxpool.Pool,
	mongoDB *mongo.Database,
	roomID string,
	executionID string,
	fileID string,
) (string, string, string, error) {
	allFiles, err := pg.GetFilesForRoom(ctx, db, roomID)
	if err != nil {
		return "", "", "", fmt.Errorf("fetch files: %w", err)
	}

	var files []pg.File
	var entryLanguage string
	for _, f := range allFiles {
		if f.ID == fileID {
			entryLanguage = f.Language
		}
		if f.IsActive {
			files = append(files, f)
		}
	}
	log.Printf("[builder] fetched %d total files, %d active, entry language=%s", len(allFiles), len(files), entryLanguage)

	var (
		contentMap sync.Map
		wg         sync.WaitGroup
		fetchErr   error
		fetchMu    sync.Mutex
	)

	docRepo := mongoRepo.NewDocumentRepository(mongoDB)

	for _, f := range files {
		if f.IsFolder {
			continue
		}
		wg.Add(1)
		go func(file pg.File) {
			defer wg.Done()

			doc, err := docRepo.LoadDocument(ctx, file.ID)
			if err != nil {
				fetchMu.Lock()
				fetchErr = fmt.Errorf("load document %s: %w", file.ID, err)
				fetchMu.Unlock()
				return
			}

			var text string
			if doc != nil && len(doc.YjsState) > 0 {
				text, err = DecodeYjsToText(doc.YjsState)
				if err != nil {
					fetchMu.Lock()
					fetchErr = fmt.Errorf("decode yjs %s: %w", file.ID, err)
					fetchMu.Unlock()
					return
				}
			}

			preview := text
			if len(preview) > 50 {
				preview = preview[:50]
			}
			log.Printf("[decoder] file=%s decoded %d chars: %q", file.Name, len(text), preview)
			contentMap.Store(file.ID, text)
		}(f)
	}

	roots := buildTree(files)
	log.Printf("[builder] tree structure:")
	printTree(roots, "  ")

	wg.Wait()
	log.Printf("[builder] all files decoded")

	if fetchErr != nil {
		return "", "", "", fetchErr
	}

	tempDir := fmt.Sprintf("/tmp/exec-%s", executionID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", "", "", fmt.Errorf("create temp dir: %w", err)
	}
	log.Printf("[builder] created temp dir: %s", tempDir)

	var entryPath string
	if err := walkTree(roots, tempDir, &contentMap, fileID, &entryPath); err != nil {
		os.RemoveAll(tempDir)
		return "", "", "", err
	}

	entryPath = strings.TrimPrefix(entryPath, tempDir+"/")

	log.Printf("[builder] file tree fully written to %s", tempDir)
	log.Printf("[builder] entry path: %s", entryPath)
	return tempDir, entryPath, entryLanguage, nil
}

func buildTree(files []pg.File) []*TreeNode {
	nodeMap := make(map[string]*TreeNode)

	for i := range files {
		nodeMap[files[i].ID] = &TreeNode{File: files[i]}
	}

	var roots []*TreeNode

	for _, node := range nodeMap {
		if node.File.ParentID == nil {
			roots = append(roots, node)
		} else {
			parent, exists := nodeMap[*node.File.ParentID]
			if !exists {
				roots = append(roots, node)
				continue
			}
			parent.Children = append(parent.Children, node)
		}
	}

	return roots
}

func printTree(nodes []*TreeNode, indent string) {
	for _, node := range nodes {
		kind := "file"
		if node.File.IsFolder {
			kind = "dir"
		}
		log.Printf("%s[%s] %s", indent, kind, node.File.Name)
		printTree(node.Children, indent+"  ")
	}
}

func walkTree(nodes []*TreeNode, currentPath string, contentMap *sync.Map, fileID string, entryPath *string) error {
	for _, node := range nodes {
		fullPath := filepath.Join(currentPath, node.File.Name)

		if node.File.ID == fileID {
			*entryPath = fullPath
		}

		if node.File.IsFolder {
			if err := os.MkdirAll(fullPath, 0755); err != nil {
				return fmt.Errorf("create folder %s: %w", fullPath, err)
			}
			log.Printf("[mkdir] %s", fullPath)
			if err := walkTree(node.Children, fullPath, contentMap, fileID, entryPath); err != nil {
				return err
			}
		} else {
			text := ""
			if val, ok := contentMap.Load(node.File.ID); ok {
				text = val.(string)
			}
			if err := os.WriteFile(fullPath, []byte(text), 0644); err != nil {
				return fmt.Errorf("write file %s: %w", fullPath, err)
			}
			log.Printf("[write] %s (%d chars)", fullPath, len(text))
		}
	}
	return nil
}