package userepo

import (
	"context"

	"github.com/sanctumlabs/curtz/app/internal/core/entities"
	"github.com/sanctumlabs/curtz/app/internal/repositories/models"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
	"github.com/sanctumlabs/curtz/app/pkg/identifier"
	"github.com/sanctumlabs/curtz/app/tools/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var log = logger.NewLogger("userRepo")

type UserRepo struct {
	dbClient *mongo.Collection
	context  context.Context
}

func NewUserRepo(dbClient *mongo.Collection, ctx context.Context) *UserRepo {
	return &UserRepo{
		dbClient: dbClient,
		context:  ctx,
	}
}

// CreateUser creates a single user
func (u *UserRepo) CreateUser(user entities.User) (entities.User, error) {
	if _, err := u.GetByEmail(user.Email.GetValue()); err == nil {
		return entities.User{}, errdefs.ErrUserExists
	}

	userModel := mapEntityToModel(user)

	_, err := u.dbClient.InsertOne(u.context, userModel)

	if err != nil {
		return user, err
	}

	return user, nil
}

// GetByEmail gets a user given an email address
func (u *UserRepo) GetByEmail(email string) (entities.User, error) {
	user, err := u.getSingleResult("email", email)
	if err != nil {
		return entities.User{}, err
	}

	return mapModelToEntity(user), nil
}

// GetById returns a user record given the id
func (u *UserRepo) GetById(id string) (entities.User, error) {
	user, err := u.getSingleResult("id", id)
	if err != nil {
		return entities.User{}, err
	}

	return mapModelToEntity(user), nil
}

// RemoveUser deletes a user record given its id
func (u *UserRepo) RemoveUser(id string) error {
	if _, err := u.GetById(id); err != nil {
		return errdefs.ErrUserDoestNotExist
	}

	filter := bson.D{{Key: "id", Value: id}}

	var result bson.D
	if err := u.dbClient.FindOneAndDelete(u.context, filter).Decode(&result); err != nil {
		return err
	}

	document, err := bson.Marshal(result)
	if err != nil {
		return err
	}

	var url models.Url
	if err = bson.Unmarshal(document, &url); err != nil {
		return err
	}

	return nil
}

func (u *UserRepo) GetByVerificationToken(verificationToken string) (entities.User, error) {
	user, err := u.getSingleResult("verification_token", verificationToken)
	if err != nil {
		return entities.User{}, err
	}

	return mapModelToEntity(user), nil
}

func (u *UserRepo) SetVerified(id identifier.ID) error {
	if _, err := u.getSingleResult("id", id.String()); err == nil {
		filter := bson.D{{Key: "id", Value: id.String()}}
		update := bson.D{{Key: "$set", Value: bson.D{{Key: "verified", Value: true}}}}
		opts := options.Update().SetUpsert(false)

		result, err := u.dbClient.UpdateOne(u.context, filter, update, opts)
		if err != nil {
			return err
		}

		log.Debugf("User %s set to verified. Result: %v", id, result.ModifiedCount)
		return nil
	} else {
		return err
	}
}

func (u *UserRepo) getSingleResult(key, value string) (models.User, error) {
	filter := bson.D{{Key: key, Value: value}}

	var result bson.D
	if err := u.dbClient.FindOne(u.context, filter).Decode(&result); err != nil {
		return models.User{}, err
	}

	document, err := bson.Marshal(result)
	if err != nil {
		return models.User{}, err
	}

	var user models.User
	err = bson.Unmarshal(document, &user)
	if err != nil {
		return user, err
	}

	return user, nil
}
