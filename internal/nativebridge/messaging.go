package nativebridge

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

func Run(ctx context.Context, input io.Reader, output io.Writer, client *Client) error {
	for {
		content, err := readFrame(input)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		var request Request
		if json.Unmarshal(content, &request) != nil {
			if err := writeFrame(output, Response{Error: "INVALID_REQUEST"}); err != nil {
				return err
			}
			continue
		}
		if err := writeFrame(output, client.Handle(ctx, request)); err != nil {
			return err
		}
	}
}

func readFrame(input io.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(input, binary.LittleEndian, &size); err != nil {
		return nil, err
	}
	if size == 0 || size > MaximumMessageSize {
		return nil, ErrInvalidMessage
	}
	content := make([]byte, size)
	_, err := io.ReadFull(input, content)
	return content, err
}

func writeFrame(output io.Writer, value Response) error {
	content, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(content) > MaximumMessageSize {
		return ErrInvalidMessage
	}
	if err := binary.Write(output, binary.LittleEndian, uint32(len(content))); err != nil {
		return err
	}
	_, err = output.Write(content)
	return err
}
