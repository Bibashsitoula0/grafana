package sqlstore

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-xorm/xorm"

	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/events"
	m "github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/setting"
	"github.com/grafana/grafana/pkg/util"
)

func init() {
	bus.AddHandler("sql", CreateUser)
	bus.AddHandler("sql", GetUserById)
	bus.AddHandler("sql", UpdateUser)
	bus.AddHandler("sql", ChangeUserPassword)
	bus.AddHandler("sql", GetUserByLogin)
	bus.AddHandler("sql", SetUsingOrg)
	bus.AddHandler("sql", GetUserInfo)
	bus.AddHandler("sql", GetSignedInUser)
	bus.AddHandler("sql", SearchUsers)
	bus.AddHandler("sql", GetUserOrgList)
	bus.AddHandler("sql", DeleteUser)
	bus.AddHandler("sql", SetUsingOrg)
}

func getOrgIdForNewUser(userEmail string, sess *session) (int64, error) {
	var org m.Org

	if setting.SingleOrgMode {
		has, err := sess.Where("name=?", setting.DefaultOrgName).Get(&org)
		if err != nil {
			return 0, err
		}
		if has {
			return org.Id, nil
		} else {
			org.Name = setting.DefaultOrgName
		}
	} else {
		org.Name = userEmail
	}

	org.Created = time.Now()
	org.Updated = time.Now()

	if _, err := sess.Insert(&org); err != nil {
		return 0, err
	}

	return org.Id, nil
}

func CreateUser(cmd *m.CreateUserCommand) error {
	return inTransaction2(func(sess *session) error {
		orgId, err := getOrgIdForNewUser(cmd.Email, sess)
		if err != nil {
			return err
		}

		// create user
		user := m.User{
			Email:   cmd.Email,
			Name:    cmd.Name,
			Login:   cmd.Login,
			Company: cmd.Company,
			IsAdmin: cmd.IsAdmin,
			OrgId:   orgId,
			Created: time.Now(),
			Updated: time.Now(),
		}

		if len(cmd.Password) > 0 {
			user.Salt = util.GetRandomString(10)
			user.Rands = util.GetRandomString(10)
			user.Password = util.EncodePassword(cmd.Password, user.Salt)
		}

		sess.UseBool("is_admin")

		if _, err := sess.Insert(&user); err != nil {
			return err
		}

		// create org user link
		orgUser := m.OrgUser{
			OrgId:   orgId,
			UserId:  user.Id,
			Role:    m.ROLE_ADMIN,
			Created: time.Now(),
			Updated: time.Now(),
		}

		if setting.SingleOrgMode && !user.IsAdmin {
			orgUser.Role = m.RoleType(setting.DefaultOrgRole)
		}

		if _, err = sess.Insert(&orgUser); err != nil {
			return err
		}

		sess.publishAfterCommit(&events.UserCreated{
			Timestamp: user.Created,
			Id:        user.Id,
			Name:      user.Name,
			Login:     user.Login,
			Email:     user.Email,
		})

		cmd.Result = user
		return nil
	})
}

func GetUserById(query *m.GetUserByIdQuery) error {
	user := new(m.User)
	has, err := x.Id(query.Id).Get(user)

	if err != nil {
		return err
	} else if has == false {
		return m.ErrUserNotFound
	}

	query.Result = user

	return nil
}

func GetUserByLogin(query *m.GetUserByLoginQuery) error {
	if query.LoginOrEmail == "" {
		return m.ErrUserNotFound
	}

	user := new(m.User)
	if strings.Contains(query.LoginOrEmail, "@") {
		user = &m.User{Email: query.LoginOrEmail}
	} else {
		user = &m.User{Login: query.LoginOrEmail}
	}

	has, err := x.Get(user)

	if err != nil {
		return err
	} else if has == false {
		return m.ErrUserNotFound
	}

	query.Result = user

	return nil
}

func UpdateUser(cmd *m.UpdateUserCommand) error {
	return inTransaction2(func(sess *session) error {

		user := m.User{
			Name:    cmd.Name,
			Email:   cmd.Email,
			Login:   cmd.Login,
			Updated: time.Now(),
		}

		if _, err := sess.Id(cmd.UserId).Update(&user); err != nil {
			return err
		}

		sess.publishAfterCommit(&events.UserUpdated{
			Timestamp: user.Created,
			Id:        user.Id,
			Name:      user.Name,
			Login:     user.Login,
			Email:     user.Email,
		})

		return nil
	})
}

func ChangeUserPassword(cmd *m.ChangeUserPasswordCommand) error {
	return inTransaction2(func(sess *session) error {

		user := m.User{
			Password: cmd.NewPassword,
			Updated:  time.Now(),
		}

		if _, err := sess.Id(cmd.UserId).Update(&user); err != nil {
			return err
		}

		return nil
	})
}

func SetUsingOrg(cmd *m.SetUsingOrgCommand) error {
	return inTransaction(func(sess *xorm.Session) error {
		user := m.User{}
		sess.Id(cmd.UserId).Get(&user)

		user.OrgId = cmd.OrgId
		_, err := sess.Id(user.Id).Update(&user)
		return err
	})
}

func GetUserInfo(query *m.GetUserInfoQuery) error {
	var user m.User
	has, err := x.Id(query.UserId).Get(&user)

	if err != nil {
		return err
	} else if has == false {
		return m.ErrUserNotFound
	}

	query.Result = m.UserDTO{
		Name:  user.Name,
		Email: user.Email,
		Login: user.Login,
	}

	return err
}

func GetUserOrgList(query *m.GetUserOrgListQuery) error {
	query.Result = make([]*m.UserOrgDTO, 0)
	sess := x.Table("org_user")
	sess.Join("INNER", "org", "org_user.org_id=org.id")
	sess.Where("org_user.user_id=?", query.UserId)
	sess.Cols("org.name", "org_user.role", "org_user.org_id")
	err := sess.Find(&query.Result)
	return err
}

func GetSignedInUser(query *m.GetSignedInUserQuery) error {
	var rawSql = `SELECT
	                u.id           as user_id,
	                u.is_admin     as is_grafana_admin,
	                u.email        as email,
	                u.login        as login,
									u.name         as name,
	                org.name       as org_name,
	                org_user.role  as org_role,
	                org.id         as org_id
	                FROM ` + dialect.Quote("user") + ` as u
									LEFT OUTER JOIN org_user on org_user.org_id = u.org_id and org_user.user_id = u.id
	                LEFT OUTER JOIN org on org.id = u.org_id
	                WHERE u.id=?`

	var user m.SignedInUser
	sess := x.Table("user")
	has, err := sess.Sql(rawSql, query.UserId).Get(&user)
	if err != nil {
		return err
	} else if !has {
		return m.ErrUserNotFound
	}

	query.Result = &user
	return err
}

func SearchUsers(query *m.SearchUsersQuery) error {
	query.Result = make([]*m.UserSearchHitDTO, 0)
	sess := x.Table("user")
	sess.Where("email LIKE ?", query.Query+"%")
	sess.Limit(query.Limit, query.Limit*query.Page)
	sess.Cols("id", "email", "name", "login", "is_admin")
	err := sess.Find(&query.Result)
	return err
}

func DeleteUser(cmd *m.DeleteUserCommand) error {
	return inTransaction(func(sess *xorm.Session) error {
		var rawSql = fmt.Sprintf("DELETE FROM %s WHERE id=?", x.Dialect().Quote("user"))
		_, err := sess.Exec(rawSql, cmd.UserId)
		return err
	})
}
