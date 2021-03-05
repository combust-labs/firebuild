package utils

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

// CreateRootFSFile uses dd to create a rootfs file of given size at a given path.
func CreateRootFSFile(path string, size int) error {
	exitCode, cmdErr := RunShellCommandNoSudo(fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=%d", path, size))
	if cmdErr != nil {
		return cmdErr
	}
	if exitCode != 0 {
		return fmt.Errorf("coomand finished with non-zero exit code")
	}
	return nil
}

// MkfsExt4 uses mkfs.ext4 to create an EXT4 file system in a given file.
func MkfsExt4(path string) error {
	exitCode, cmdErr := RunShellCommandNoSudo(fmt.Sprintf("mkfs.ext4 %s", path))
	if cmdErr != nil {
		return cmdErr
	}
	if exitCode != 0 {
		return fmt.Errorf("coomand finished with non-zero exit code")
	}
	return nil
}

// Mount sudo mounts a rootfs file at a location.
func Mount(file, dir string) error {
	exitCode, cmdErr := RunShellCommandSudo(fmt.Sprintf("mount %s %s", file, dir))
	if cmdErr != nil {
		return cmdErr
	}
	if exitCode != 0 {
		return fmt.Errorf("command finished with non-zero exit code")
	}
	return nil
}

// MoveFile moves file from one location to another
// and create intermediate directories.
// If the target file already exists, it will be truncated to size 0 first.
func MoveFile(source, target string) error {

	// TODO: implement a backup mechanism

	success := false

	sourceStat, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !sourceStat.Mode().IsRegular() {
		return fmt.Errorf("source is not regular file")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0664); err != nil {
		return err
	}

	// open writable file:
	writableFile, err := os.OpenFile(target, os.O_RDWR, 0664)
	if err != nil {
		return err
	}
	defer func() {
		if err := writableFile.Close(); err != nil {
			fmt.Printf("failed closing writable file, reason: %+v", err)
		}
		if !success {
			// write failed, remove the file...
			if err := os.Remove(target); err != nil {
				fmt.Printf("failed removing target file on unsuccessful move, trash left, reason: %+v", err)
			}
		}
	}()

	if err := writableFile.Truncate(0); err != nil {
		return err
	}

	// open readable file:
	readableFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() {
		if err := readableFile.Close(); err != nil {
			fmt.Printf("failed closing readable file, reason: %+v", err)
		}
		if success {
			// write failed, remove the file...
			if err := os.Remove(source); err != nil {
				fmt.Printf("failed removing source file on successful move, trash left, reason: %+v", err)
			}
		}
	}()

	// rewrite the file:
	buf := make([]byte, 8*1024*1024)
	for {
		read, err := readableFile.Read(buf)
		if read == 0 && err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading source file, reason: %+v", err)
		}
		written, err := writableFile.Write(buf[0:read])
		if err != nil {
			return fmt.Errorf("error writing target file, reason: %+v", err)
		}
		if written != read {
			fmt.Println(fmt.Sprintf("warning, written '%d' vs read '%d' did not match", written, read))
		}
	}

	success = true

	return nil
}

// RunShellCommandNoSudo runs a shell command without sudo.
func RunShellCommandNoSudo(command string) (int, error) {
	return runShellCommand(command, false)
}

// RunShellCommandSudo runs a shell command with sudo.
func RunShellCommandSudo(command string) (int, error) {
	return runShellCommand(command, true)
}

// Umount sudo umounts a location.
func Umount(dir string) error {
	exitCode, cmdErr := RunShellCommandSudo(fmt.Sprintf("umount %s", dir))
	if cmdErr != nil {
		return cmdErr
	}
	if exitCode != 0 {
		return fmt.Errorf("command finished with non-zero exit code")
	}
	return nil
}

// --

func runShellCommand(command string, sudo bool) (int, error) {
	if sudo {
		command = fmt.Sprintf("sudo %s", command)
	}
	cmd := exec.Command("/bin/sh", []string{`-c`, command}...)
	cmd.Stderr = os.Stderr
	stdOut, err := cmd.StdoutPipe()
	if err != nil {
		return 1, fmt.Errorf("failed redirecting stdout: %+v", err)
	}
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("failed command start: %+v", err)
	}
	_, readErr := ioutil.ReadAll(stdOut)
	if readErr != nil {
		return 1, fmt.Errorf("failed reading output: %+v", readErr)
	}
	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode(), exitError
		}
		return 1, fmt.Errorf("failed waiting for command: %+v", err)
	}
	return 0, nil
}
