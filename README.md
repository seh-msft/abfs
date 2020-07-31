# abfs

Abfs provides a platform/network agnostic expression of an Azure blob storage container as a directory hierarchy which could be interacted with as though it were any other directory on a system.

A user should be able to list, create, edit, and delete files in an Azure blob storage container and view these over a 9p file system.

A reasonable way of testing this could be through a Linux VM with support for FUSE and utilize 9pfuse, 9pfs, or the v9fs kernel module. WSL2 could probably mount 9p using 9pfuse, but WSL1 lacks FUSE support.

On Windows natively, testing for 9p could be done through [the Inferno virtual machine](http://doc.cat-v.org/inferno/). The [purgatorio fork](https://github.com/9mirrors/purgatorio) runs on Windows 10 and builds in VS2019. 

## Dependencies

Written in Golang: https://golang.org

Imports:

- `github.com/Azure/azure-storage-blob-go`
- `aqwari.net/net/styx`

## Build

	go build

## Test

	go test

## Run

	abfs

## Usage

The `>` rune indicates a command run in Powershell. 

The `$` rune indicates a command run in bash(1) under WSL. 

The `;` rune indicates a command run in sh(1) under Inferno. 

### Serve over tcp locally and leverage from Inferno

From Windows Powershell:

	> $Env:AZURE_STORAGE_ACCOUNT = "<youraccountname>"
	> $Env:AZURE_STORAGE_ACCESS_KEY = "<youraccountkey>"
	> abfs -p ':1337'
	...

From Windows Powershell to Inferno:

	> emu
	; mount -A 'tcp!127.0.0.1!1337' /n/abfs
	; ls /n/abfs
	/n/abfs/foo
	;

### Serve over tcp locally and leverage from WSL

From Windows Powershell:

	> $Env:AZURE_STORAGE_ACCOUNT = "<youraccountname>"
	> $Env:AZURE_STORAGE_ACCESS_KEY = "<youraccountkey>"
	> abfs -p ':1337'
	...

From WSL using [plan9port's](https://9fans.github.io/plan9port/) [9p(1) command](https://9fans.github.io/plan9port/man/man1/9p.html):

	$ 9p -a 'tcp!127.0.0.1!1337' ls
	foo
	$

## Functionality

The file system is currently very slow due to the unreasonably large number of sync operations due to a lack of remote change detection before making further calls. 

### Implemented and working

- Create 
- Read
- Write
- Delete
- Stat
- TCP listening

### Not Implemented

- Wstat
- Nested directories
- Listening on protocols/interfaces other than TCP

## Contribute

Please PR on GitHub :)

Please comment all functions, structs, and major - shared - variables. 

Please run `go fmt` and `go vet` on source before submitting the PR. 

## History

Abfs was written for and during the 2020 hackathon at Microsoft. 

## References

- https://github.com/droyo/jsonfs
for `styx` 9p
- https://github.com/rjkroege/edwood for Windows and 9p at once
- https://docs.microsoft.com/en-us/azure/storage/blobs/storage-quickstart-blobs-go
- https://github.com/Azure-Samples/storage-blobs-go-quickstart

## Tools 

- https://github.com/mischief/9pfs
- https://9fans.github.io/plan9port/man/man4/9pfuse.html
- https://www.kernel.org/doc/html/latest/filesystems/9p.html
- https://github.com/9mirrors/purgatorio
