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

// Package internal ...
package internal

import (
	"context"
	awsutils "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/identitystore/types"
	"io/ioutil"
	"strings"

	"github.com/awslabs/ssosync/internal/aws"
	"github.com/awslabs/ssosync/internal/config"
	"github.com/awslabs/ssosync/internal/google"
	log "github.com/sirupsen/logrus"
	admin "google.golang.org/api/admin/directory/v1"
)

// SyncGSuite is the interface for synchronizing users/groups
type SyncGSuite interface {
	SyncUsers(string) (*UserSyncResult, error)
	SyncGroups(string, *UserSyncResult) error
	RemoveUsers([]*types.User) error
}

// SyncGSuite is an object type that will synchronize real users and groups
type syncGSuite struct {
	aws    aws.Client
	google google.Client
	cfg    *config.Config
}

type UserSyncResult struct {
	index         map[string]*types.User
	toDelete      []*types.User
	indexByUserId map[string]*types.User
}

// New will create a new SyncGSuite object
func New(cfg *config.Config, a aws.Client, g google.Client) SyncGSuite {
	return &syncGSuite{
		aws:    a,
		google: g,
		cfg:    cfg,
	}
}

// SyncUsers will Sync Google Users to AWS SSO SCIM
// References:
// * https://developers.google.com/admin-sdk/directory/v1/guides/search-users
// query possible values:
// '' --> empty or not defined
//  name:'Jane'
//  email:admin*
//  isAdmin=true
//  manager='janesmith@example.com'
//  orgName=Engineering orgTitle:Manager
//  EmploymentData.projects:'GeneGnomes'
func (s *syncGSuite) SyncUsers(query string) (*UserSyncResult, error) {
	log.Debug("get all users from amazon")
	usersSyncResult := &UserSyncResult{
		index:         make(map[string]*types.User),
		toDelete:      []*types.User{},
		indexByUserId: make(map[string]*types.User),
	}
	awsUsers, err := s.aws.GetUsers()
	if err != nil {
		log.Error("Error Getting AWS Users: ", err)
		return usersSyncResult, err
	}
	for _, u := range awsUsers {
		userToAdd := u
		usersSyncResult.index[awsutils.ToString(u.UserName)] = &userToAdd
		usersSyncResult.indexByUserId[awsutils.ToString(u.UserId)] = &userToAdd
	}

	log.Debug("get deleted users")
	gcpDeletedUsers, err := s.google.GetDeletedUsers()
	if err != nil {
		log.Error("Error Getting Deleted Users from Google: ", err)
		return usersSyncResult, err
	}

	for _, u := range gcpDeletedUsers {
		ll := log.WithFields(log.Fields{"email": u.PrimaryEmail})
		ll.Info("Adding users to deleting from gcpDeletedUsers")
		userInAWS, isExists := usersSyncResult.index[u.PrimaryEmail]

		if isExists == false {
			ll.Debug("User already deleted")
			continue
		}

		ll.Warn("User added to delete")
		usersSyncResult.toDelete = append(usersSyncResult.toDelete, userInAWS)
	}

	log.Debug("get active google users")
	googleUsers, err := s.google.GetUsers(query)
	if err != nil {
		return usersSyncResult, err
	}

	for _, u := range googleUsers {
		if s.ignoreUser(u.PrimaryEmail) {
			continue
		}

		ll := log.WithFields(log.Fields{"email": u.PrimaryEmail})
		ll.Debug("finding user")
		userInAWS, isExists := usersSyncResult.index[u.PrimaryEmail]
		if isExists == true {
			if u.Suspended == true {
				ll.Warn("User added to delete as suspended in Google")
				usersSyncResult.toDelete = append(usersSyncResult.toDelete, userInAWS)
			} else {
				ll.Debug("Did nothing, user already added")
			}
		} else {
			if u.Suspended == true {
				ll.Debug("Did nothing, as User suspended in Google")
			} else {
				userToAdd := &types.User{
					UserName:    awsutils.String(u.PrimaryEmail),
					DisplayName: awsutils.String(strings.Join([]string{u.Name.GivenName, u.Name.FamilyName}, " ")),
					Name: &types.Name{
						FamilyName: awsutils.String(u.Name.FamilyName),
						GivenName:  awsutils.String(u.Name.GivenName),
					},
					Emails: []types.Email{
						{
							Primary: true,
							Type:    awsutils.String("work"),
							Value:   awsutils.String(u.PrimaryEmail),
						},
					},
					ExternalIds: []types.ExternalId{
						{
							Id:     awsutils.String(u.Id),
							Issuer: awsutils.String("Google"),
						},
					},
				}
				ll.Debug("Create user")
				added, err := s.aws.CreateUser(userToAdd)
				if err != nil {
					ll.Error("Can't create user: ", err)
					//return usersSyncResult, err
				} else {
					usersSyncResult.index[u.PrimaryEmail] = added
					usersSyncResult.indexByUserId[awsutils.ToString(added.UserId)] = added
				}
			}
		}
	}
	return usersSyncResult, nil
}

// SyncGroups will sync groups from Google -> AWS SSO
// References:
// * https://developers.google.com/admin-sdk/directory/v1/guides/search-groups
// query possible values:
// '' --> empty or not defined
//  name='contact'
//  email:admin*
//  memberKey=user@company.com
//  name:contact* email:contact*
//  name:Admin* email:aws-*
//  email:aws-*
func (s *syncGSuite) SyncGroups(query string, usersSyncResult *UserSyncResult) error {
	log.Debug("get all groups from amazon")
	awsGroups, err := s.aws.GetGroups()
	if err != nil {
		log.Warn("Error Getting AWS Groups")
		return err
	}

	groupsIndex := make(map[string]*types.Group)
	var groupsToDelete []*types.Group
	for _, u := range awsGroups {
		grp := u
		groupsIndex[awsutils.ToString(u.DisplayName)] = &grp
	}

	log.WithField("query", query).Debug("get google groups")
	googleGroups, err := s.google.GetGroups(query)
	if err != nil {
		return err
	}

	googleGroupsIndex := make(map[string]*admin.Group)

	for _, g := range googleGroups {
		if s.ignoreGroup(g.Email) {
			continue
		}
		googleGroupsIndex[g.Name] = g

		ll := log.WithFields(log.Fields{"group": g.Name})
		ll.Debug("Check group")

		_, isExists := groupsIndex[g.Name]
		if isExists == true {
			ll.Debug("Did nothing, group already exists")
		} else {
			ll.Debug("Creating group")
			gg, err := s.aws.CreateGroup(awsutils.String(g.Name), awsutils.String(g.Description))
			if err != nil {
				ll.Error("Can't create Group in AWS: ", err)
			} else {
				groupsIndex[awsutils.ToString(gg.DisplayName)] = gg
			}
		}
	}

	for _, g := range awsGroups {
		_, isExists := googleGroupsIndex[awsutils.ToString(g.DisplayName)]
		if isExists == false {
			grp := g
			groupsToDelete = append(groupsToDelete, &grp)
			delete(groupsIndex, awsutils.ToString(g.DisplayName))
		}
	}

	for _, g := range groupsIndex {
		val, _ := googleGroupsIndex[awsutils.ToString(g.DisplayName)]
		err := s.SyncMembershipsForGroup(val, g, usersSyncResult)
		if err != nil {
			return err
		}
	}

	for _, g := range groupsToDelete {
		err := s.aws.DeleteGroup(g)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *syncGSuite) SyncMembershipsForGroup(googleGroup *admin.Group, awsGroup *types.Group,
	usersSyncResult *UserSyncResult) error {
	ll := log.WithField("group", googleGroup.Name)

	ll.Info("Fetching google groups")
	groupMembers, err := s.google.GetGroupMembers(googleGroup)
	if err != nil {
		ll.Info("Can't fetch google groups")
		return err
	}
	memberList := make(map[string]*types.User)
	for _, m := range groupMembers {
		if val, ok := usersSyncResult.index[m.Email]; ok {
			memberList[m.Email] = val
		}
	}
	ll.Info("Fetching aws groups")
	awsMembers, err := s.aws.GetGroupMembers(awsGroup)
	if err != nil {
		ll.Info("Can't fetch AWS groups")
		return err
	}

	var toDelete []*types.GroupMembership
	for _, m := range awsMembers {
		awsMember := m
		userId, ok := m.MemberId.(*types.MemberIdMemberUserId)
		if ok != true {
			ll.Error("Cast mismatch error")
		}
		user, exists := usersSyncResult.indexByUserId[userId.Value]
		if exists == false {
			toDelete = append(toDelete, &awsMember)
		} else {
			_, has := memberList[awsutils.ToString(user.UserName)]
			if has == false {
				toDelete = append(toDelete, &awsMember)
			}
			delete(memberList, awsutils.ToString(user.UserName))
		}
	}

	for _, val := range toDelete {
		err := s.aws.RemoveGroupMembership(val)
		if err != nil {
			ll.Error("Can't remove User from the group: ", err)
			return err
		}
	}

	for _, element := range memberList {
		_, err := s.aws.AddUserToGroup(element, awsGroup)
		if err != nil {
			ll.Error("Can't add User to the group: ", err)
			return err
		}
	}
	return nil
}

// DoSync will create a logger and run the sync with the paths
// given to do the sync.
func DoSync(ctx context.Context, cfg *config.Config) error {
	log.Info("Syncing AWS users and groups from Google Workspace SAML Application")

	creds := []byte(cfg.GoogleCredentials)

	if !cfg.IsLambda {
		b, err := ioutil.ReadFile(cfg.GoogleCredentials)
		if err != nil {
			return err
		}
		creds = b
	}

	googleClient, err := google.NewClient(ctx, cfg.GoogleAdmin, creds)
	if err != nil {
		return err
	}

	awsClient := aws.NewClient(
		cfg.AWSConfig,
		cfg.IdentityStoreId)

	c := New(cfg, awsClient, googleClient)

	syncResult, err := c.SyncUsers(cfg.UserMatch)
	if err != nil {
		return err
	}

	err = c.SyncGroups(cfg.GroupMatch, syncResult)
	if err != nil {
		return err
	}

	return c.RemoveUsers(syncResult.toDelete)
}

func (s *syncGSuite) RemoveUsers(usersList []*types.User) error {
	for _, u := range usersList {
		err := s.aws.DeleteUser(u)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *syncGSuite) ignoreUser(name string) bool {
	for _, u := range s.cfg.IgnoreUsers {
		if u == name {
			return true
		}
	}

	return false
}

func (s *syncGSuite) ignoreGroup(name string) bool {
	for _, g := range s.cfg.IgnoreGroups {
		if g == name {
			return true
		}
	}

	return false
}
