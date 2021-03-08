// This code comes from Terraform SSH communicator:
// https://github.com/hashicorp/terraform/blob/f172585eaa23b9edc40c5b73b066654e1bae7120/communicator/ssh/communicator.go#L514
// This file is explicitly excluded from the top repository license.

package remote

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	"golang.org/x/crypto/ssh"
)

func (dcc *defaultConnectedClient) scpSession(scpCommand string, f func(io.Writer, *bufio.Reader) error) error {

	session, err := dcc.sshClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	// Get a pipe to stdin so that we can send data down
	stdinW, err := session.StdinPipe()
	if err != nil {
		return err
	}

	// We only want to close once, so we nil w after we close it,
	// and only close in the defer if it hasn't been closed already.
	defer func() {
		if stdinW != nil {
			stdinW.Close()
		}
	}()

	// Get a pipe to stdout so that we can get responses back
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	stdoutR := bufio.NewReader(stdoutPipe)

	// Set stderr to a bytes buffer
	stderr := new(bytes.Buffer)
	session.Stderr = stderr

	// Start the sink mode on the other side
	// TODO(mitchellh): There are probably issues with shell escaping the path
	dcc.logger.Info("Starting remote scp process:", "command", scpCommand)
	if err := session.Start(scpCommand); err != nil {
		return err
	}

	// Call our callback that executes in the context of SCP. We ignore
	// EOF errors if they occur because it usually means that SCP prematurely
	// ended on the other side.
	dcc.logger.Debug("Started SCP session, beginning transfers...")
	if err := f(stdinW, stdoutR); err != nil && err != io.EOF {
		return err
	}

	// Close the stdin, which sends an EOF, and then set w to nil so that
	// our defer func doesn't close it again since that is unsafe with
	// the Go SSH package.
	dcc.logger.Debug("SCP session complete, closing stdin pipe.")
	stdinW.Close()
	stdinW = nil

	// Wait for the SCP connection to close, meaning it has consumed all
	// our data and has completed. Or has errored.
	dcc.logger.Debug("Waiting for SSH session to complete.")
	err = session.Wait()

	// log any stderr before exiting on an error
	scpErr := stderr.String()
	if len(scpErr) > 0 {
		dcc.logger.Error("[scp stderr:", "reason", stderr)
	}

	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			// Otherwise, we have an ExitErorr, meaning we can just read
			// the exit status
			dcc.logger.Error(exitErr.Error())

			// If we exited with status 127, it means SCP isn't available.
			// Return a more descriptive error for that.
			if exitErr.ExitStatus() == 127 {
				return errors.New(
					"SCP failed to start. This usually means that SCP is not\n" +
						"properly installed on the remote system.")
			}
		}

		return err
	}

	return nil
}

// checkSCPStatus checks that a prior command sent to SCP completed
// successfully. If it did not complete successfully, an error will
// be returned.
func checkSCPStatus(r *bufio.Reader) error {
	code, err := r.ReadByte()
	if err != nil {
		return err
	}

	if code != 0 {
		// Treat any non-zero (really 1 and 2) as fatal errors
		message, _, err := r.ReadLine()
		if err != nil {
			return fmt.Errorf("Error reading error message: %s", err)
		}

		return errors.New(string(message))
	}

	return nil
}

func scpUploadFile(logger hclog.Logger, mode fs.FileMode, dst string, src io.Reader, w io.Writer, r *bufio.Reader, size int64) error {
	if size == 0 {
		// Create a temporary file where we can copy the contents of the src
		// so that we can determine the length, since SCP is length-prefixed.
		tf, err := ioutil.TempFile("", "terraform-upload")
		if err != nil {
			return fmt.Errorf("Error creating temporary file for upload: %s", err)
		}
		defer os.Remove(tf.Name())
		defer tf.Close()

		logger.Debug("Copying input data into temporary file so we can read the length")
		if _, err := io.Copy(tf, src); err != nil {
			return err
		}

		// Sync the file so that the contents are definitely on disk, then
		// read the length of it.
		if err := tf.Sync(); err != nil {
			return fmt.Errorf("Error creating temporary file for upload: %s", err)
		}

		// Seek the file to the beginning so we can re-read all of it
		if _, err := tf.Seek(0, 0); err != nil {
			return fmt.Errorf("Error creating temporary file for upload: %s", err)
		}

		fi, err := tf.Stat()
		if err != nil {
			return fmt.Errorf("Error creating temporary file for upload: %s", err)
		}

		src = tf
		size = fi.Size()
	}

	// Start the protocol
	logger.Debug("Beginning file upload...")
	// PRESERVE THE MODE:
	fmt.Fprintln(w, fmt.Sprintf("C0%s", fileModeToString(mode)), size, dst)
	if err := checkSCPStatus(r); err != nil {
		return err
	}

	if _, err := io.Copy(w, src); err != nil {
		return err
	}

	fmt.Fprint(w, "\x00")
	if err := checkSCPStatus(r); err != nil {
		return err
	}

	return nil
}

func scpUploadDirProtocol(logger hclog.Logger, mode fs.FileMode, name string, w io.Writer, r *bufio.Reader, f func() error) error {
	logger.Debug("SCP: starting directory upload", "directory-name", name)
	// PRESERVE THE MODE:
	fmt.Fprintln(w, fmt.Sprintf("D0 0%s", fileModeToString(mode)), name)
	err := checkSCPStatus(r)
	if err != nil {
		return err
	}

	if err := f(); err != nil {
		return err
	}

	fmt.Fprintln(w, "E")
	if err != nil {
		return err
	}

	return nil
}

func scpUploadDir(logger hclog.Logger, root string, fs []os.FileInfo, w io.Writer, r *bufio.Reader) error {
	for _, fi := range fs {
		realPath := filepath.Join(root, fi.Name())

		// Track if this is actually a symlink to a directory. If it is
		// a symlink to a file we don't do any special behavior because uploading
		// a file just works. If it is a directory, we need to know so we
		// treat it as such.
		isSymlinkToDir := false
		if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
			symPath, err := filepath.EvalSymlinks(realPath)
			if err != nil {
				return err
			}

			symFi, err := os.Lstat(symPath)
			if err != nil {
				return err
			}

			isSymlinkToDir = symFi.IsDir()
		}

		if !fi.IsDir() && !isSymlinkToDir {
			// It is a regular file (or symlink to a file), just upload it
			f, err := os.Open(realPath)
			if err != nil {
				return err
			}

			err = func() error {
				defer f.Close()
				return scpUploadFile(logger, fi.Mode().Perm(), fi.Name(), f, w, r, fi.Size())
			}()

			if err != nil {
				return err
			}

			continue
		}

		// It is a directory, recursively upload
		err := scpUploadDirProtocol(logger, fi.Mode().Perm(), fi.Name(), w, r, func() error {
			f, err := os.Open(realPath)
			if err != nil {
				return err
			}
			defer f.Close()

			entries, err := f.Readdir(-1)
			if err != nil {
				return err
			}

			return scpUploadDir(logger, realPath, entries, w, r)
		})
		if err != nil {
			return err
		}
	}

	return nil
}
