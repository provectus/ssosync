// Copyright (c) 2020, Amazon.com, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package aws

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	store "github.com/aws/aws-sdk-go-v2/service/identitystore"
	"github.com/aws/aws-sdk-go-v2/service/identitystore/types"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrGroupNotFound     = errors.New("group not found")
	ErrNoGroupsFound     = errors.New("no groups found")
	ErrUserNotSpecified  = errors.New("user not specified")
	ErrGroupNotSpecified = errors.New("group not specified")
)

// Client represents an interface of methods used
// to communicate with AWS SSO
type Client interface {
	CreateUser(*types.User) (*types.User, error)
	DeleteUser(*types.User) error
	DeleteGroup(*types.Group) error
	CreateGroup(name *string, description *string) (*types.Group, error)
	AddUserToGroup(*types.User, *types.Group) (*types.GroupMembership, error)
	RemoveGroupMembership(membership *types.GroupMembership) error
	GetGroupMembers(*types.Group) ([]types.GroupMembership, error)
	GetGroups() ([]types.Group, error)
	GetUsers() ([]types.User, error)
}

type client struct {
	identityStore   *store.Client
	identityStoreId *string
}

// NewClient creates a new client to talk with AWS SSO's Identity Store.
func NewClient(config aws.Config, identityStoreId string) Client {
	return &client{
		identityStore:   store.NewFromConfig(config),
		identityStoreId: &identityStoreId,
	}
}

// CreateUser will create the user specified
func (c *client) CreateUser(u *types.User) (*types.User, error) {
	res, err := c.identityStore.CreateUser(context.TODO(),
		&store.CreateUserInput{
			IdentityStoreId: c.identityStoreId,
			DisplayName:     u.DisplayName,
			UserName:        u.UserName,
			Name:            u.Name,
			Emails:          u.Emails,
		})

	if err != nil {
		return nil, err
	}

	u.UserId = res.UserId
	return u, err
}

// DeleteUser will remove the current user from the directory
func (c *client) DeleteUser(u *types.User) error {
	_, err := c.identityStore.DeleteUser(context.TODO(),
		&store.DeleteUserInput{
			IdentityStoreId: c.identityStoreId,
			UserId:          u.UserId,
		})
	return err
}

// DeleteGroup will delete the group specified
func (c *client) DeleteGroup(g *types.Group) error {
	_, err := c.identityStore.DeleteGroup(context.TODO(),
		&store.DeleteGroupInput{
			GroupId:         g.GroupId,
			IdentityStoreId: c.identityStoreId,
		})

	return err
}

// CreateGroup will create a group given
func (c *client) CreateGroup(name *string, description *string) (*types.Group, error) {
	res, err := c.identityStore.CreateGroup(context.TODO(),
		&store.CreateGroupInput{
			IdentityStoreId: c.identityStoreId,
			DisplayName:     name,
			Description:     description,
		})
	if err != nil {
		return nil, err
	}
	group := &types.Group{
		GroupId:     res.GroupId,
		DisplayName: name,
		Description: description,
	}
	return group, err
}

// AddUserToGroup will add the user specified to the group specified
func (c *client) AddUserToGroup(u *types.User, g *types.Group) (*types.GroupMembership, error) {
	memberId := &types.MemberIdMemberUserId{
		Value: aws.ToString(u.UserId),
	}
	res, err := c.identityStore.CreateGroupMembership(context.TODO(),
		&store.CreateGroupMembershipInput{
			GroupId:         g.GroupId,
			MemberId:        memberId,
			IdentityStoreId: c.identityStoreId,
		})
	if err != nil {
		return nil, err
	}

	result := &types.GroupMembership{
		IdentityStoreId: c.identityStoreId,
		GroupId:         g.GroupId,
		MemberId:        memberId,
		MembershipId:    res.MembershipId,
	}
	return result, err
}

// RemoveGroupMembership will remove the user specified from the group specified
func (c *client) RemoveGroupMembership(membership *types.GroupMembership) error {
	_, err := c.identityStore.DeleteGroupMembership(context.TODO(),
		&store.DeleteGroupMembershipInput{
			IdentityStoreId: c.identityStoreId,
			MembershipId:    membership.MembershipId,
		})
	return err
}

// GetGroupMembers will return existing groups
func (c *client) GetGroupMembers(g *types.Group) ([]types.GroupMembership, error) {
	var res []types.GroupMembership
	paginator := store.NewListGroupMembershipsPaginator(c.identityStore,
		&store.ListGroupMembershipsInput{
			IdentityStoreId: c.identityStoreId,
			MaxResults:      aws.Int32(50),
			GroupId:         g.GroupId,
		})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(context.TODO())
		if err != nil {
			return res, err
		}
		res = append(res, output.GroupMemberships...)
	}
	return res, nil
}

// GetGroups will return existing groups
func (c *client) GetGroups() ([]types.Group, error) {
	var res []types.Group
	paginator := store.NewListGroupsPaginator(c.identityStore,
		&store.ListGroupsInput{
			IdentityStoreId: c.identityStoreId,
			MaxResults:      aws.Int32(50),
		})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(context.TODO())
		if err != nil {
			return res, err
		}
		res = append(res, output.Groups...)
	}
	return res, nil
}

// GetUsers will return existing users
func (c *client) GetUsers() ([]types.User, error) {
	var res []types.User
	paginator := store.NewListUsersPaginator(c.identityStore,
		&store.ListUsersInput{
			IdentityStoreId: c.identityStoreId,
			MaxResults:      aws.Int32(50),
			NextToken:       nil,
		})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(context.TODO())
		if err != nil {
			return res, err
		}
		res = append(res, output.Users...)
	}
	return res, nil
}
