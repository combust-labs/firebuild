package server

import (
	"bytes"
	"crypto/sha256"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/grpc/proto"
	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/build/resources"
	"github.com/gofrs/uuid"
)

type GRPCReadingDirectoryResource interface {
	WalkResource() chan *proto.ResourceChunk
}

// NewResolvedDirectoryResourceWithPath creates a resolved resource from input information containing resource source path.
func NewGRPCDirectoryResource(safeBufferSize int, resource resources.ResolvedResource) GRPCReadingDirectoryResource {
	return &grpcDirectoryResource{contentsReader: func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bytes.NewReader([]byte{})), nil
	},
		isDir:          true,
		resolved:       resource.ResolvedURIOrPath(),
		safeBufferSize: safeBufferSize,
		targetMode:     resource.TargetMode(),
		sourcePath:     resource.SourcePath(),
		targetPath:     resource.TargetPath(),
		targetWorkdir:  resource.TargetWorkdir(),
		targetUser:     resource.TargetUser(),
	}
}

type grpcDirectoryResource struct {
	contentsReader func() (io.ReadCloser, error)
	isDir          bool
	resolved       string
	safeBufferSize int
	targetMode     fs.FileMode
	sourcePath     string
	targetPath     string
	targetWorkdir  commands.Workdir
	targetUser     commands.User
}

func (drr *grpcDirectoryResource) WalkResource() chan *proto.ResourceChunk {
	chanChunks := make(chan *proto.ResourceChunk)
	go func() {
		filepath.WalkDir(drr.resolved, func(path string, d fs.DirEntry, err error) error {

			finfo, err := d.Info()
			if err != nil {
				return err
			}

			remainingPath := strings.TrimPrefix(strings.TrimPrefix(path, drr.resolved), "/")

			resourceUUID := uuid.Must(uuid.NewV4()).String()

			if d.IsDir() {
				chanChunks <- &proto.ResourceChunk{
					Payload: &proto.ResourceChunk_Header{
						Header: &proto.ResourceChunk_ResourceHeader{
							SourcePath:    filepath.Join(drr.sourcePath, remainingPath),
							TargetPath:    filepath.Join(drr.targetPath, remainingPath),
							FileMode:      int64(finfo.Mode().Perm()),
							IsDir:         true,
							TargetUser:    drr.targetUser.Value,
							TargetWorkdir: drr.targetWorkdir.Value,
							Id:            resourceUUID,
						},
					},
				}
				chanChunks <- &proto.ResourceChunk{
					Payload: &proto.ResourceChunk_Eof{
						Eof: &proto.ResourceChunk_ResourceEof{
							Id: resourceUUID,
						},
					},
				}
				return nil
			}

			// it's a file:

			chanChunks <- &proto.ResourceChunk{
				Payload: &proto.ResourceChunk_Header{
					Header: &proto.ResourceChunk_ResourceHeader{
						SourcePath:    filepath.Join(drr.sourcePath, remainingPath),
						TargetPath:    filepath.Join(drr.targetPath, remainingPath),
						FileMode:      int64(finfo.Mode().Perm()),
						IsDir:         true,
						TargetUser:    drr.targetUser.Value,
						TargetWorkdir: drr.targetWorkdir.Value,
						Id:            resourceUUID,
					},
				},
			}

			buffer := make([]byte, drr.safeBufferSize)

			reader, err := os.Open(path)
			defer reader.Close()

			for {
				readBytes, err := reader.Read(buffer)
				if readBytes == 0 && err == io.EOF {
					chanChunks <- &proto.ResourceChunk{
						Payload: &proto.ResourceChunk_Eof{
							Eof: &proto.ResourceChunk_ResourceEof{
								Id: resourceUUID,
							},
						},
					}
					break
				} else {
					payload := buffer[0:readBytes]
					hash := sha256.Sum256(payload)
					chanChunks <- &proto.ResourceChunk{
						Payload: &proto.ResourceChunk_Chunk{
							Chunk: &proto.ResourceChunk_ResourceContents{
								Chunk:    payload,
								Checksum: hash[:],
								Id:       resourceUUID,
							},
						},
					}
				}
			}

			return nil
		})
		chanChunks <- nil
	}()
	return chanChunks
}
