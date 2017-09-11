package rpc

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"

	"github.com/pkg/errors"
)

type Client struct {
	ml     *MessageLayer
	logger Logger
}

func NewClient(rwc io.ReadWriteCloser) *Client {
	return &Client{NewMessageLayer(rwc), noLogger{}}
}

func (c *Client) SetLogger(logger Logger, logMessageLayer bool) {
	c.logger = logger
	if logMessageLayer {
		c.ml.logger = logger
	} else {
		c.ml.logger = noLogger{}
	}
}

func (c *Client) Close() (err error) {

	c.logger.Printf("sending Close request")
	header := Header{
		DataType: DataTypeControl,
		Endpoint: ControlEndpointClose,
		Accept:   DataTypeControl,
	}
	err = c.ml.WriteHeader(&header)
	if err != nil {
		return
	}

	c.logger.Printf("reading Close ACK")
	ack, err := c.ml.ReadHeader()
	if err != nil {
		return err
	}
	c.logger.Printf("received Close ACK: %#v", ack)
	if ack.Error != StatusOK {
		err = errors.Errorf("error hanging up: remote error (%s) %s", ack.Error, ack.ErrorMessage)
		return
	}

	c.logger.Printf("closing MessageLayer")
	if err = c.ml.Close(); err != nil {
		c.logger.Printf("error closing RWC: %+v", err)
		return
	}

	return err
}

func (c *Client) recvResponse() (h *Header, err error) {
	h, err = c.ml.ReadHeader()
	if err != nil {
		return nil, errors.Wrap(err, "cannot read header")
	}
	// TODO validate
	return
}

func (c *Client) writeRequest(h *Header) (err error) {
	// TODO validate
	err = c.ml.WriteHeader(h)
	if err != nil {
		return errors.Wrap(err, "cannot write header")
	}
	return
}

func (c *Client) Call(endpoint string, in, out interface{}) (err error) {

	var accept DataType
	{
		outType := reflect.TypeOf(out)
		if typeIsIOReaderPtr(outType) {
			accept = DataTypeOctets
		} else {
			accept = DataTypeMarshaledJSON
		}
	}

	h := Header{
		Endpoint: endpoint,
		DataType: DataTypeMarshaledJSON,
		Accept:   accept,
	}

	if err = c.writeRequest(&h); err != nil {
		return err
	}

	var buf bytes.Buffer
	if err = json.NewEncoder(&buf).Encode(in); err != nil {
		panic("cannot encode 'in' parameter")
	}
	if err = c.ml.WriteData(&buf); err != nil {
		return err
	}

	rh, err := c.recvResponse()
	if err != nil {
		return err
	}
	if rh.Error != StatusOK {
		return &RPCError{rh}
	}

	rd := c.ml.ReadData()

	switch accept {
	case DataTypeOctets:
		c.logger.Printf("setting out to ML data reader")
		outPtr := out.(*io.Reader) // we checked that above
		*outPtr = rd
	case DataTypeMarshaledJSON:
		c.logger.Printf("decoding marshaled json")
		if err = json.NewDecoder(c.ml.ReadData()).Decode(out); err != nil {
			return errors.Wrap(err, "cannot decode marshaled reply")
		}
	default:
		panic("implementation error") // accept is controlled by us
	}

	return
}
