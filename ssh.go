package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSHPool manages SSH connections to remote hosts
type SSHPool struct {
	mu    sync.RWMutex
	conns map[string]*SSHConnection
}

// SSHConnection holds an SSH client and SFTP client
type SSHConnection struct {
	client *ssh.Client
	sftp   *sftp.Client
}

// NewSSHPool creates a new SSH connection pool
func NewSSHPool() *SSHPool {
	return &SSHPool{
		conns: make(map[string]*SSHConnection),
	}
}

// connectionKey returns a unique key for a repository connection
func connectionKey(repo *Repository) string {
	return fmt.Sprintf("%s@%s:%d", repo.User, repo.Host, repo.Port)
}

// GetRemoteFS returns a RemoteFS for the given repository
func (p *SSHPool) GetRemoteFS(repo *Repository) (*RemoteFS, error) {
	conn, err := p.getConnection(repo)
	if err != nil {
		return nil, err
	}
	return &RemoteFS{
		conn:     conn,
		basePath: repo.Path,
		repo:     repo,
	}, nil
}

// getConnection gets or creates an SSH connection for a repository
func (p *SSHPool) getConnection(repo *Repository) (*SSHConnection, error) {
	key := connectionKey(repo)

	// Check if connection exists
	p.mu.RLock()
	conn, ok := p.conns[key]
	p.mu.RUnlock()

	if ok {
		// Verify connection is still alive
		_, _, err := conn.client.SendRequest("keepalive@openssh.com", true, nil)
		if err == nil {
			return conn, nil
		}
		// Connection dead, remove it
		p.mu.Lock()
		delete(p.conns, key)
		p.mu.Unlock()
		log.Printf("SSH connection to %s died, reconnecting...", key)
	}

	// Create new connection
	conn, err := p.connect(repo)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.conns[key] = conn
	p.mu.Unlock()

	return conn, nil
}

// connect creates a new SSH connection
func (p *SSHPool) connect(repo *Repository) (*SSHConnection, error) {
	// Read SSH key
	keyPath := repo.KeyFile
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH key %s: %w", keyPath, err)
	}

	// Parse private key
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH key %s: %w", keyPath, err)
	}

	// SSH config
	config := &ssh.ClientConfig{
		User: repo.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: proper host key verification
		Timeout:         10 * time.Second,
	}

	// Connect
	addr := fmt.Sprintf("%s:%d", repo.Host, repo.Port)
	log.Printf("Connecting to SSH %s...", addr)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	// Create SFTP client
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to create SFTP client: %w", err)
	}

	log.Printf("SSH connection established to %s", addr)
	return &SSHConnection{
		client: client,
		sftp:   sftpClient,
	}, nil
}

// Close closes all connections in the pool
func (p *SSHPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for key, conn := range p.conns {
		conn.sftp.Close()
		conn.client.Close()
		log.Printf("Closed SSH connection to %s", key)
	}
	p.conns = make(map[string]*SSHConnection)
}

// RemoteFS implements FileSystem for SSH/SFTP repositories
type RemoteFS struct {
	conn     *SSHConnection
	basePath string
	repo     *Repository
}

func (r *RemoteFS) ReadFile(path string) ([]byte, error) {
	fullPath := filepath.Join(r.basePath, path)
	// Convert to forward slashes for remote
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")

	file, err := r.conn.sftp.Open(fullPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	data := make([]byte, stat.Size())
	_, err = file.Read(data)
	if err != nil && err.Error() != "EOF" {
		return nil, err
	}

	return data, nil
}

func (r *RemoteFS) ReadDir(path string) ([]fs.DirEntry, error) {
	fullPath := filepath.Join(r.basePath, path)
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")

	infos, err := r.conn.sftp.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	entries := make([]fs.DirEntry, len(infos))
	for i, info := range infos {
		entries[i] = &sftpDirEntry{info: info}
	}
	return entries, nil
}

func (r *RemoteFS) Stat(path string) (fs.FileInfo, error) {
	fullPath := filepath.Join(r.basePath, path)
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")

	return r.conn.sftp.Stat(fullPath)
}

func (r *RemoteFS) Walk(root string, fn filepath.WalkFunc) error {
	fullPath := filepath.Join(r.basePath, root)
	fullPath = strings.ReplaceAll(fullPath, "\\", "/")

	return r.walkDir(fullPath, fn)
}

func (r *RemoteFS) walkDir(path string, fn filepath.WalkFunc) error {
	info, err := r.conn.sftp.Stat(path)
	if err != nil {
		return fn(path, nil, err)
	}

	if err := fn(path, info, nil); err != nil {
		if err == filepath.SkipDir {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return nil
	}

	entries, err := r.conn.sftp.ReadDir(path)
	if err != nil {
		return fn(path, info, err)
	}

	for _, entry := range entries {
		childPath := path + "/" + entry.Name()
		if entry.IsDir() {
			if err := r.walkDir(childPath, fn); err != nil {
				return err
			}
		} else {
			if err := fn(childPath, entry, nil); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *RemoteFS) BasePath() string {
	return r.basePath
}

func (r *RemoteFS) Type() string {
	return "ssh"
}

func (r *RemoteFS) Info() map[string]string {
	return map[string]string{
		"type": "ssh",
		"host": r.repo.Host,
		"user": r.repo.User,
		"path": r.basePath,
	}
}

// sftpDirEntry wraps os.FileInfo to implement fs.DirEntry
type sftpDirEntry struct {
	info os.FileInfo
}

func (e *sftpDirEntry) Name() string               { return e.info.Name() }
func (e *sftpDirEntry) IsDir() bool                { return e.info.IsDir() }
func (e *sftpDirEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e *sftpDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }
