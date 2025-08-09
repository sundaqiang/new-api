package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"one-api/common"
	"one-api/model"
	"strconv"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type DingTalkOAuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

type DingTalkUser struct {
	UnionId   string `json:"unionId"`
	OpenId    string `json:"openId"`
	Nick      string `json:"nick"`
	AvatarUrl string `json:"avatarUrl"`
}

type DingTalkUserInfoResponse struct {
	ErrCode int          `json:"errcode"`
	ErrMsg  string       `json:"errmsg"`
	User    DingTalkUser `json:"user_info"`
}

func getDingTalkUserInfoByCode(code string) (*DingTalkUser, error) {
	if code == "" {
		return nil, errors.New("无效的参数")
	}

	// Step 1: Get access token
	tokenValues := map[string]string{
		"appid":      common.DingTalkClientId,
		"appsecret":  common.DingTalkClientSecret,
		"code":       code,
		"grant_type": "authorization_code",
	}
	jsonData, err := json.Marshal(tokenValues)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://oapi.dingtalk.com/sns/gettoken", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := http.Client{
		Timeout: 5 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		common.SysLog(err.Error())
		return nil, errors.New("无法连接至钉钉服务器，请稍后重试！")
	}
	defer res.Body.Close()

	var oAuthResponse DingTalkOAuthResponse
	err = json.NewDecoder(res.Body).Decode(&oAuthResponse)
	if err != nil {
		return nil, err
	}

	if oAuthResponse.AccessToken == "" {
		return nil, errors.New("获取访问令牌失败")
	}

	// Step 2: Get user info
	req, err = http.NewRequest("POST", "https://oapi.dingtalk.com/sns/getuserinfo", bytes.NewBuffer([]byte(`{}`)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", oAuthResponse.AccessToken))

	res2, err := client.Do(req)
	if err != nil {
		common.SysLog(err.Error())
		return nil, errors.New("无法连接至钉钉服务器，请稍后重试！")
	}
	defer res2.Body.Close()

	var userResponse DingTalkUserInfoResponse
	err = json.NewDecoder(res2.Body).Decode(&userResponse)
	if err != nil {
		return nil, err
	}

	if userResponse.ErrCode != 0 {
		return nil, errors.New(fmt.Sprintf("获取用户信息失败: %s", userResponse.ErrMsg))
	}

	if userResponse.User.UnionId == "" {
		return nil, errors.New("返回值非法，用户字段为空，请稍后重试！")
	}

	return &userResponse.User, nil
}

func DingTalkOAuth(c *gin.Context) {
	session := sessions.Default(c)
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "state is empty or not same",
		})
		return
	}
	username := session.Get("username")
	if username != nil {
		DingTalkBind(c)
		return
	}

	if !common.DingTalkOAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启通过钉钉登录以及注册",
		})
		return
	}

	code := c.Query("code")
	dingtalkUser, err := getDingTalkUserInfoByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	user := model.User{
		DingTalkId: dingtalkUser.UnionId,
	}

	// IsDingTalkIdAlreadyTaken is unscoped
	if model.IsDingTalkIdAlreadyTaken(user.DingTalkId) {
		// FillUserByDingTalkId is scoped
		err := user.FillUserByDingTalkId()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		// if user.Id == 0 , user has been deleted
		if user.Id == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "用户已注销",
			})
			return
		}
	} else {
		if common.RegisterEnabled {
			user.Username = "dingtalk_" + strconv.Itoa(model.GetMaxUserId()+1)
			if dingtalkUser.Nick != "" {
				user.DisplayName = dingtalkUser.Nick
			} else {
				user.DisplayName = "DingTalk User"
			}
			user.Role = common.RoleCommonUser
			user.Status = common.UserStatusEnabled
			affCode := session.Get("aff")
			inviterId := 0
			if affCode != nil {
				inviterId, _ = model.GetUserIdByAffCode(affCode.(string))
			}

			if err := user.Insert(inviterId); err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				return
			}
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员关闭了新用户注册",
			})
			return
		}
	}

	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "用户已被封禁",
			"success": false,
		})
		return
	}
	setupLogin(&user, c)
}

func DingTalkBind(c *gin.Context) {
	if !common.DingTalkOAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启通过钉钉登录以及注册",
		})
		return
	}
	code := c.Query("code")
	dingtalkUser, err := getDingTalkUserInfoByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user := model.User{
		DingTalkId: dingtalkUser.UnionId,
	}
	if model.IsDingTalkIdAlreadyTaken(user.DingTalkId) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该钉钉账户已被绑定",
		})
		return
	}
	session := sessions.Default(c)
	id := session.Get("id")
	// id := c.GetInt("id")  // critical bug!
	user.Id = id.(int)
	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user.DingTalkId = dingtalkUser.UnionId
	err = user.Update(false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "bind",
	})
	return
}
