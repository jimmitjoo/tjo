package data

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"net/http"
	"strings"
	"time"

	up "github.com/upper/db/v4"
)

type Token struct {
	ID        int       `db:"id,omitempty" json:"id"`
	UserID    int       `db:"user_id" json:"user_id"`
	FirstName string    `db:"first_name" json:"first_name"`
	Email     string    `db:"email" json:"email"`
	PlainText string    `db:"token" json:"token"`
	Hash      []byte    `db:"token_hash" json:"-"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
	ExpiresAt time.Time `db:"expiry" json:"expires_at"`
}

func (t *Token) Table() string {
	return "tokens"
}

func (t *Token) GetUserForToken(token string) (*User, error) {
	var user User
	var theToken Token

	collection := upper.Collection(t.Table())
	res := collection.Find(up.Cond{"token =": token})
	err := res.One(&theToken)

	if err != nil {
		return nil, err
	}

	collection = upper.Collection(user.Table())
	res = collection.Find(up.Cond{"id =": theToken.UserID})
	err = res.One(&user)

	if err != nil {
		return nil, err
	}

	user.Token = theToken

	return &user, nil
}

// get tokens for a user
func (t *Token) GetTokensForUser(id int) ([]*Token, error) {
	collection := upper.Collection(t.Table())

	var tokens []*Token

	res := collection.Find(up.Cond{"user_id =": id}).OrderBy("created_at desc")
	err := res.All(&tokens)

	if err != nil {
		return nil, err
	}

	return tokens, nil
}

// get a token by id
func (t *Token) Find(id int) (*Token, error) {
	collection := upper.Collection(t.Table())

	var token Token

	res := collection.Find(up.Cond{"id =": id})
	err := res.One(&token)

	if err != nil {
		return nil, err
	}

	return &token, nil
}

// get token by token
func (t *Token) GetByToken(token string) (*Token, error) {
	collection := upper.Collection(t.Table())

	var theToken Token

	res := collection.Find(up.Cond{"token =": token})
	err := res.One(&theToken)

	if err != nil {
		return nil, err
	}

	return &theToken, nil
}

// delete token by id
func (t *Token) Delete(id int) error {
	collection := upper.Collection(t.Table())

	err := collection.Find(id).Delete()

	return err
}

// delete token by token
func (t *Token) DeleteByToken(token string) error {
	collection := upper.Collection(t.Table())

	err := collection.Find(up.Cond{"token =": token}).Delete()

	return err
}

// insert token by token and user
func (t *Token) Insert(token Token, user User) error {
	collection := upper.Collection(t.Table())

	// delete user's existing tokens
	err := collection.Find(up.Cond{"user_id =": user.ID}).Delete()

	if err != nil {
		return err
	}

	token.CreatedAt = time.Now()
	token.UpdatedAt = time.Now()
	token.FirstName = user.FirstName
	token.Email = user.Email

	_, err = collection.Insert(token)

	return err
}

// generate a token for a user with a ttl
func (t *Token) GenerateToken(userId int, ttl time.Duration) (*Token, error) {
	var token Token

	token.UserID = userId
	token.ExpiresAt = time.Now().Add(ttl)

	randomToken := make([]byte, 16)
	_, err := rand.Read(randomToken)
	if err != nil {
		return nil, err
	}

	token.PlainText = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomToken)
	hash := sha256.Sum256([]byte(token.PlainText))
	token.Hash = hash[:]
	token.CreatedAt = time.Now()
	token.UpdatedAt = time.Now()

	return &token, nil
}

// Authenticate a token
func (t *Token) AuthenticateToken(r *http.Request) (*User, error) {
	authirazationHeader := r.Header.Get("Authorization")
	if authirazationHeader == "" {
		return nil, errors.New("no authorization header received")
	}

	headerParts := strings.Split(authirazationHeader, " ")
	if len(headerParts) != 2 || headerParts[0] != "Bearer" {
		return nil, errors.New("invalid authorization header")
	}

	token := headerParts[1]

	if len(token) != 26 {
		return nil, errors.New("invalid token length")
	}

	tkn, err := t.GetByToken(token)

	if err != nil {
		return nil, errors.New("no matching token found")
	}

	if tkn.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("token has expired")
	}

	user, err := t.GetUserForToken(token)

	if err != nil {
		return nil, errors.New("no matching user found")
	}

	return user, nil
}

// validate a token
func (t *Token) ValidateToken(token string) (bool, error) {
	user, err := t.GetUserForToken(token)

	if err != nil {
		return false, errors.New("no matching user found")
	}

	if user.Token.PlainText == "" {
		return false, errors.New("no matching token found")
	}

	if user.Token.ExpiresAt.Before(time.Now()) {
		return false, errors.New("token has expired")
	}

	if len(token) != 26 {
		return false, errors.New("invalid token length")
	}

	return true, nil
}
