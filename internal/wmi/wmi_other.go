//go:build !windows

package wmi

import "context"

// Client is a placeholder outside Windows; every call returns ErrUnavailable.
type Client struct{}

// New returns a Client. Outside Windows it can only report ErrUnavailable.
func New() *Client { return &Client{} }

func (c *Client) Query(ctx context.Context, namespace, wql string) ([]Row, error) {
	if err := CheckQuery(wql); err != nil {
		return nil, err
	}
	return nil, ErrUnavailable
}

func (c *Client) Call(ctx context.Context, namespace, objectPath, method string, in map[string]any) (Row, error) {
	if err := CheckMethod(method); err != nil {
		return nil, err
	}
	return nil, ErrUnavailable
}
