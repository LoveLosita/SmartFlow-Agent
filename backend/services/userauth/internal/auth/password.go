package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword 负责将明文密码转换为 bcrypt 哈希结果。
//
// 职责边界：
// 1. 只负责密码哈希，不负责用户名校验、持久化和错误翻译。
// 2. 输入是明文密码，输出是可直接入库的 bcrypt 字符串。
func HashPassword(pwd string) (string, error) {
	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPwd), nil
}

// CompareHashPwdAndPwd 负责校验明文密码与 bcrypt 哈希是否匹配。
//
// 职责边界：
// 1. 只负责密码比对，不负责用户存在性判断和登录态处理。
// 2. 当密码不匹配时返回 false, nil；只有底层异常才返回非 nil error。
func CompareHashPwdAndPwd(hashedPwd, pwd string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPwd), []byte(pwd))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
