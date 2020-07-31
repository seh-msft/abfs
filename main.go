// Copyright (c) 2020 Microsoft Corporation, Sean Hinchee.
// Licensed under the MIT License.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"aqwari.net/net/styx"
	"github.com/Azure/azure-storage-blob-go/azblob"
)

const (
	maxBlobs = 4096 // Maximum number of blobs to track
)

var (
	//announce      = flag.String("a", "tcp!localhost!1337", "Dialstring to announce on") // TODO
	containerName = flag.String("c", "9pfs", "Name of container to fs-ify")
	port          = flag.String("p", ":1337", "TCP port to listen for 9p connections")
	chatty        = flag.Bool("D", false, "Chatty 9p tracing")
	verbose       = flag.Bool("V", false, "Verbose 9p error output")
)

// A 9p file server exposing an azure blob container
func main() {
	flag.Parse()

	var (
		styxServer styx.Server // 9p file server handle for styx
		srv        Server      // Our file system server
	)

	srv.Initialize()

	log.Printf("Using %s as the container for the fs...\n", *containerName)

	/* Set up Azure */

	// Acquire azure credential information from environment variables
	accountName := os.Getenv("AZURE_STORAGE_ACCOUNT")
	accountKey := os.Getenv("AZURE_STORAGE_ACCESS_KEY")
	if len(accountName) == 0 || len(accountKey) == 0 {
		fatal("$AZURE_STORAGE_ACCOUNT and $AZURE_STORAGE_ACCESS_KEY environment variables must be set to authenticate")
	}

	// Create a new azure auth pipeline
	credential, err := azblob.NewSharedKeyCredential(accountName, accountKey)
	if err != nil {
		fatal("err: could not authenticate - ", err)
	}
	p := azblob.NewPipeline(credential, azblob.PipelineOptions{})

	/* Set up the storage container */

	urlStr, err := url.Parse(fmt.Sprintf("https://%s.blob.core.windows.net/%s", accountName, *containerName))
	if err != nil {
		fatal("err: could not generate container URL - ", *urlStr)
	}

	container := azblob.NewContainerURL(*urlStr, p)
	ctx := context.Background()

	srv.container = container
	srv.ctx = ctx

	// We only need the error
	_, err = container.Create(ctx, azblob.Metadata{}, azblob.PublicAccessNone)
	exists := false

	if err != nil {
		// We have to search the error for the magic response string
		if strings.Contains(err.Error(), string(azblob.ServiceCodeContainerAlreadyExists)) {
			exists = true
		}

		// The container didn't exist, but we couldn't create it
		if !exists {
			fatal("err: could not create container - ", err)
		}
	}

	if exists {
		log.Println(`Container "` + *containerName + `" found, using...`)
	} else {
		log.Println(`No existing container "` + *containerName + `", creating...`)
	}

	/* Populate tree with contents from the container */

	var names []string

	// Skip population if the container didn't exist, there's nothing contained
	if !exists {
		goto Styx
	}

	log.Println("Reading existing blobs from container...")

	// List all remote blobs
	names, err = ListBlobs(&srv)
	if err != nil {
		fatal("err: could not list remote blobs - ", err)
	}

	if len(names) < 1 {
		log.Println("No extant blobs found, continuing...")
		goto Styx
	}

	log.Printf("Found %d extant blobs, populating fs...\n", len(names))

	// Insert blobs into file tree
	// TODO - some kind of nested directory handling?
	for _, name := range names {
		f, err := srv.Insert("/"+name, false)
		if err != nil {
			fatal("err: could not insert extant blobs into fs - ", err)
		}

		// TODO - lazy download - we only need meta-info, not the whole file
		f.Blob.Download(srv.ctx)
	}

	/* Set up 9p server */
Styx:

	if *chatty {
		styxServer.TraceLog = log.New(os.Stderr, "", 0)
	}
	if *verbose {
		styxServer.ErrorLog = log.New(os.Stderr, "", 0)
	}

	// TODO - actually parse dial string (new module?)
	// TODO - allow options like /srv posting, unix socket, etc.
	//proto, addr, port := dialstring.Parse(*announce)
	styxServer.Addr = *port

	// Shim our own logger, in case we need it
	styxServer.Handler = styx.Stack(logger, &srv)

	fatal(styxServer.ListenAndServe())
}
