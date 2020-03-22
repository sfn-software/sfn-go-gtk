package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
)

var ln net.Listener
var conn net.Conn
var reader *bufio.Reader
var writer *bufio.Writer

const BufferSize = 102400

func Listen(port string) (string, error) {
	var err error
	err = StopListen()
	log.Println("listening...")

	// listen on port 8000
	ln, err = net.Listen("tcp", ":"+port)
	if err != nil || ln == nil {
		return "", err
	}

	// accept connection
	conn, err = ln.Accept()
	if err != nil {
		return "", err
	}

	reader = bufio.NewReader(conn)
	writer = bufio.NewWriter(conn)

	return conn.RemoteAddr().String(), nil
}

func StopListen() error {
	if ln != nil {
		err := ln.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func Connect(address string) (string, error) {
	var err error
	conn, err = net.Dial("tcp", address)
	if err != nil {
		return "", err
	}

	reader = bufio.NewReader(conn)
	writer = bufio.NewWriter(conn)

	return conn.RemoteAddr().String(), nil
}

func ReadFile(path string, nl func(name string, size int64), pl func(p int)) (bool, error) {
	t, err := reader.ReadByte()
	if assertError(err, "unable to read type") {
		return false, err
	}
	log.Println("type:", t)
	switch t {
	case 1:
		line, isPrefix, err := reader.ReadLine()
		if assertError(err, "unable to read name") {
			return false, err
		}
		log.Println(string(line), isPrefix)
		var size int64
		err = binary.Read(reader, binary.LittleEndian, &size)
		if assertError(err, "unable to read size") {
			return false, err
		}

		name := filepath.Base(string(line))
		nl(name, size)
		log.Println("create file:", filepath.Join(path, name))
		file, err := os.Create(filepath.Join(path, name))
		if assertError(err, "unable to create file") {
			return false, err
		}

		buffer := make([]byte, BufferSize)
		var total int64 = 0
		p := 0
		for {
			if total+int64(len(buffer)) > size {
				buffer = make([]byte, size-total)
				log.Println("resize buffer to", size-total)
			}
			n, err := reader.Read(buffer)
			if err != nil {
				if err == io.EOF {
					break
				}
				assertError(err, "file read error")
				return false, err
			}
			total += int64(n)
			n, err = file.Write(buffer[:n])
			if err != nil {
				panic("file write error")
			}
			if int(100*total/size) != p {
				p = int(100 * total / size)
				pl(p)
			}
			if total == size {
				break
			}
		}
		err = file.Close()
		if assertError(err, "file close error") {
			return false, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func SendFile(name string, l func(p int)) error {
	base := filepath.Base(name)
	stat, err := os.Stat(name)
	if assertError(err, "unable to get file info") {
		return err
	}
	size := stat.Size()
	err = writer.WriteByte(1)
	if assertError(err, "event type sending failed") {
		return err
	}
	_, err = writer.WriteString(base + "\n")
	if assertError(err, "file name sending failed") {
		return err
	}

	buf := new(bytes.Buffer)
	if err = binary.Write(buf, binary.LittleEndian, size); err != nil {
		assertError(err, "file size preparing failed")
		return err
	}
	_, err = buf.WriteTo(writer)
	if assertError(err, "file size sending failed") {
		return err
	}

	err = writer.Flush()
	if assertError(err, "header flushing failed") {
		return err
	}

	file, err := os.Open(name)
	if assertError(err, "unable to open file") {
		return err
	}
	buffer := make([]byte, BufferSize)
	var total int64 = 0
	p := 0
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			assertError(err, "local file read error")
			return err
		}
		total += int64(n)
		n, err = writer.Write(buffer[:n])
		if assertError(err, "file write to socket error") {
			return err
		}
		err = writer.Flush()
		if assertError(err, "file data flushing error") {
			return err
		}
		if int(100*total/size) != p {
			p = int(100 * total / size)
			l(p)
		}
	}
	err = writer.Flush()
	if assertError(err, "data flushing error") {
		return err
	}
	err = file.Close()
	if assertError(err, "file closing error") {
		return err
	}
	return nil
}

func SendDone() error {
	err := writer.WriteByte(2)
	if assertError(err, "done sending failed") {
		return err
	}
	err = writer.Flush()
	if assertError(err, "done flushing failed") {
		return err
	}
	return nil
}

func Disconnect() error {
	if conn != nil {
		err := conn.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func assertError(err error, message string) bool {
	if err != nil {
		log.Fatalln(message, err)
		return true
	}
	return false
}
