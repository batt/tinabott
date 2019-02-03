package brain

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"

	log "github.com/Sirupsen/logrus"
	"github.com/go-redis/redis"
)

type Brain struct {
	client *redis.Client
}

func New(url string) *Brain {

	client := redis.NewClient(&redis.Options{
		Addr:     url,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	pong, err := client.Ping().Result()
	log.Println(pong)
	if err != nil {
		log.Fatalln(err.Error)
	}

	return &Brain{client: client}
}

func (b *Brain) Set(key string, val interface{}) error {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	err := enc.Encode(val)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	return b.client.Set(key, encoded, 0).Err()
}

func (b *Brain) Get(key string, q interface{}) error {
	val, err := b.client.Get("key").Result()

	if err != nil {
		return err
	}
	decoded, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		return err
	}
	buf := bytes.NewBuffer(decoded)
	dec := gob.NewDecoder(buf)
	return dec.Decode(&q)
}
