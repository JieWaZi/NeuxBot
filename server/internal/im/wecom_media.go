package im

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// wecomMediaURL 限定企微官方 API 与腾讯媒体域名，不能下载用户提供的任意 URL。
func wecomMediaURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "qyapi.weixin.qq.com" || strings.HasSuffix(host, ".qpic.cn") || (strings.HasSuffix(host, ".myqcloud.com") && strings.Contains(host, ".cos."))
}

// Download 有界读取并按官方 AES-256-CBC、32 字节 PKCS#7 规则解密媒体。
func (w *Wecom) Download(ctx context.Context, _ models.IMConnection, _ string, file models.IMFile) ([]byte, error) {
	if !wecomMediaURL(file.Key) {
		return nil, status_error.IMFileInvalid
	}
	key, err := base64.StdEncoding.DecodeString(file.AESKey)
	if err != nil {
		// 官方 Node SDK 同样接受省略尾部等号的标准 Base64 密钥。
		key, err = base64.RawStdEncoding.DecodeString(file.AESKey)
	}
	if err != nil || len(key) != 32 {
		return nil, status_error.IMProtocolInvalid
	}
	client := w.client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 || !wecomMediaURL(req.URL.String()) {
				return status_error.IMProtocolInvalid
			}
			return nil
		}}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.Key, nil)
	if err != nil {
		return nil, status_error.IMFileInvalid
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() == nil {
			logx.Errorf("wecom media download failed: error_type=%T", err)
		}
		return nil, status_error.IMPlatformUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, status_error.IMPlatformUnavailable
	}
	encrypted, err := readMediaLimit(response.Body, constants.IMFileMaximumBytes+32)
	if err != nil {
		return nil, err
	}
	if len(encrypted) == 0 || len(encrypted)%aes.BlockSize != 0 {
		return nil, status_error.IMProtocolInvalid
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, status_error.IMProtocolInvalid
	}
	value := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(value, encrypted)
	padding := int(value[len(value)-1])
	if padding < 1 || padding > 32 || padding > len(value) || !bytes.Equal(value[len(value)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return nil, status_error.IMProtocolInvalid
	}
	value = value[:len(value)-padding]
	if int64(len(value)) > constants.IMFileMaximumBytes {
		return nil, status_error.IMFileInvalid
	}
	return value, nil
}

// upload 仅上传已冻结的受管文件；上传成功仍不代表聊天消息送达。
func (s *wecomSession) upload(ctx context.Context, file models.IMFile) (string, string, error) {
	source, err := os.Open(file.Path)
	if err != nil {
		logx.Errorf("wecom frozen media open failed: error_type=%T", err)
		return "", "", status_error.IMFileInvalid
	}
	defer source.Close()
	value, err := ReadMedia(source)
	if err != nil {
		return "", "", err
	}
	if len(value) == 0 {
		return "", "", status_error.IMFileInvalid
	}
	kind := "file"
	if file.Kind == "image" && len(value) <= 10*1024*1024 {
		kind = "image"
	}
	const chunkSize = 512 * 1024
	chunks := (len(value) + chunkSize - 1) / chunkSize
	hash := md5.Sum(value)
	call := func(command string, body any) (wecomFrame, error) {
		result, definitive, err := s.request(ctx, command, command+"_"+util.New(), body)
		if !definitive && errors.Is(err, status_error.IMPlatformUnavailable) {
			// 素材上传没有提交聊天消息；传输或上传 ACK 不确定时可从头上传。
			// 明确非零平台回执保留拒绝分类，不能因此永久自动重试。
			return result, status_error.IMServiceUnavailable
		}
		return result, err
	}
	result, err := call(constants.IMWecomUploadInit, map[string]any{"type": kind, "filename": SafeFilename(file.Name), "total_size": len(value), "total_chunks": chunks, "md5": hex.EncodeToString(hash[:])})
	if err != nil {
		return "", "", err
	}
	var uploaded struct {
		// UploadID 是官方返回的本次临时素材上传身份。
		UploadID string `json:"upload_id"`
		// MediaID 是完成上传后用于消息发送的素材身份。
		MediaID string `json:"media_id"`
	}
	if json.Unmarshal(result.Body, &uploaded) != nil || uploaded.UploadID == "" {
		return "", "", status_error.IMProtocolInvalid
	}
	for index := 0; index < chunks; index++ {
		end := min((index+1)*chunkSize, len(value))
		// 索引遵循官方 SDK 实现，从零开始；串行分片复用已有连接，无额外上传协程。
		_, err = call(constants.IMWecomUploadChunk, map[string]any{"upload_id": uploaded.UploadID, "chunk_index": index, "base64_data": base64.StdEncoding.EncodeToString(value[index*chunkSize : end])})
		if err != nil {
			return "", "", err
		}
	}
	result, err = call(constants.IMWecomUploadFinish, map[string]string{"upload_id": uploaded.UploadID})
	if err != nil {
		return "", "", err
	}
	if json.Unmarshal(result.Body, &uploaded) != nil || uploaded.MediaID == "" {
		return "", "", status_error.IMProtocolInvalid
	}
	return uploaded.MediaID, kind, nil
}
