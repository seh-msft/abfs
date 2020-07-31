// Copyright (c) 2020 Microsoft Corporation, Sean Hinchee.
// Licensed under the MIT License.

// Contains Azure blob information for use with File
package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"time"

	"github.com/Azure/azure-storage-blob-go/azblob"
)

const (
	// See: https://godoc.org/github.com/Azure/azure-storage-blob-go/azblob#UploadStreamToBlockBlob
	maxBuffers = 3               // Max # rotating buffers for upload
	bufSize    = 2 * 1024 * 1024 // Rotating buffer size for upload
	maxRetry   = 20              // Maximum number of retries for download
)

// Tracks a blob and its state
type Blob struct {
	// TODO - way to check for changes in Azure
	name *string             // Ref to File.name
	last time.Time           // Time last accessed by us
	body bytes.Buffer        // Bytes contents of file
	url  azblob.BlockBlobURL // Azure blob URL
}

// List remote Azure blobs by name
func ListBlobs(srv *Server) ([]string, error) {
	names := make([]string, 0, maxBlobs)

	for marker := (azblob.Marker{}); marker.NotDone(); {
		blob, err := srv.container.ListBlobsFlatSegment(srv.ctx, marker, azblob.ListBlobsSegmentOptions{})
		if err != nil {
			return nil, errors.New("could not list blobs from container - " + err.Error())
		}

		// Shift forwards to the next marker in the set of blobs
		marker = blob.NextMarker

		for _, info := range blob.Segment.BlobItems {
			names = append(names, info.Name)
		}
	}

	return names, nil
}

// Create a new blob
func NewBlob(name *string, container azblob.ContainerURL) *Blob {
	url := container.NewBlockBlobURL(*name)

	return &Blob{
		name: name,
		last: time.Now(),
		url:  url,
	}
}

// Return the contents of the body buffer
func (b Blob) Contents() []byte {
	// TODO - sync with Azure to verify state?
	return b.body.Bytes()
}

// Upload a blob in full
func (b *Blob) Upload(ctx context.Context) error {
	log.Println("!!!! UPLOADING ", *b.name)
	opts := azblob.UploadStreamToBlockBlobOptions{
		BufferSize: bufSize,
		MaxBuffers: maxBuffers,
	}

	_, err := azblob.UploadStreamToBlockBlob(ctx, bytes.NewReader(b.body.Bytes()), b.url, opts)

	return err
}

// Download a blob in full
func (b *Blob) Download(ctx context.Context) error {
	log.Println("!!!! DOWNLOADING", *b.name)
	resp, err := b.url.Download(ctx, 0, azblob.CountToEnd, azblob.BlobAccessConditions{}, false)

	opts := azblob.RetryReaderOptions{
		MaxRetryRequests: maxRetry,
	}

	bodyStream := resp.Body(opts)
	b.body.Reset()

	// Read the body into a buffer
	_, err = b.body.ReadFrom(bodyStream)

	return err
}
