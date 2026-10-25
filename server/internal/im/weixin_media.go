package im

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Download 验证腾讯 CDN 来源后下载并按当前媒体协议解密。
func (w *Weixin) Download(ctx context.Context, _ models.IMConnection, _ string, file models.IMFile) ([]byte, error) {
	if !OfficialURL(file.Key, true) {
		return nil, status_error.IMFileInvalid
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.Key, nil)
	if err != nil {
		return nil, status_error.IMFileInvalid
	}
	response, err := HTTPClient(true).Do(request)
	if err != nil {
		return nil, status_error.IMPlatformUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, status_error.IMPlatformUnavailable
	}
	limit := constants.IMFileMaximumBytes
	if file.AESKey != "" {
		// 传输预算包含 PKCS#7 填充，文件预算仍按解密后的实际字节计算。
		limit += aes.BlockSize
	}
	value, err := readMediaLimit(response.Body, limit)
	if err != nil {
		return nil, err
	}
	if file.AESKey == "" {
		return value, nil
	}
	key, err := base64.StdEncoding.DecodeString(file.AESKey)
	if err != nil {
		return nil, status_error.IMProtocolInvalid
	}
	if len(key) == 32 {
		key, err = hex.DecodeString(string(key))
		if err != nil {
			return nil, status_error.IMProtocolInvalid
		}
	}
	value, err = cryptWeixinMedia(value, key, false)
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > constants.IMFileMaximumBytes {
		return nil, status_error.IMFileInvalid
	}
	return value, nil
}

func (w *Weixin) upload(ctx context.Context, connection models.IMConnection, binding models.IMBinding, file models.IMFile) (weixinItem, error) {
	var item weixinItem
	source, err := os.Open(file.Path)
	if err != nil {
		return item, status_error.IMFileInvalid
	}
	defer source.Close()
	value, err := ReadMedia(source)
	if err != nil {
		return item, err
	}
	key := make([]byte, aes.BlockSize)
	if _, err := rand.Read(key); err != nil {
		return item, status_error.IMCredentialError
	}
	encrypted, err := cryptWeixinMedia(value, key, true)
	if err != nil {
		return item, err
	}
	fileKey := StableID(binding.ID, hex.EncodeToString(key))
	hash := md5.Sum(value)
	mediaType := 3
	if file.Kind == "image" {
		mediaType = 1
	}
	var response weixinResponse
	_, err = w.request(ctx, weixinBase(connection), "/ilink/bot/getuploadurl", http.MethodPost, connection.Credential, map[string]any{"filekey": fileKey, "media_type": mediaType, "to_user_id": binding.ExternalChatID, "rawsize": len(value), "rawfilemd5": hex.EncodeToString(hash[:]), "filesize": len(encrypted), "no_need_thumb": true, "aeskey": hex.EncodeToString(key), "base_info": weixinInfo()}, &response)
	if err != nil {
		return item, err
	}
	if response.Ret != 0 || response.ErrorCode != 0 {
		return item, status_error.IMPlatformUnavailable
	}
	uploadURL := response.UploadURL
	if uploadURL == "" && response.UploadParam != "" {
		uploadURL = constants.IMWeixinCDNBaseURL + "/upload?encrypted_query_param=" + url.QueryEscape(response.UploadParam) + "&filekey=" + url.QueryEscape(fileKey)
	}
	if !OfficialURL(uploadURL, true) {
		return item, status_error.IMProtocolInvalid
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(encrypted))
	if err != nil {
		return item, status_error.IMProtocolInvalid
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	upload, err := HTTPClient(true).Do(request)
	if err != nil {
		return item, status_error.IMPlatformUnavailable
	}
	defer upload.Body.Close()
	query := upload.Header.Get("x-encrypted-param")
	if upload.StatusCode != http.StatusOK || query == "" {
		return item, status_error.IMPlatformUnavailable
	}
	media := weixinMediaItem{Media: weixinMedia{Query: query, AESKey: base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(key))), Type: 1}, Name: file.Name, Length: strconv.Itoa(len(value))}
	item = weixinItem{Type: 4, File: &media}
	if file.Kind == "image" {
		media.Size = len(encrypted)
		item = weixinItem{Type: 2, Image: &media}
	}
	return item, nil
}

// cryptWeixinMedia 仅用于腾讯媒体要求的 AES-128-ECB 与严格 PKCS#7 填充。
func cryptWeixinMedia(value, key []byte, encrypt bool) ([]byte, error) {
	if len(key) != aes.BlockSize {
		return nil, status_error.IMProtocolInvalid
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, status_error.IMProtocolInvalid
	}
	if encrypt {
		padding := aes.BlockSize - len(value)%aes.BlockSize
		value = append(append([]byte(nil), value...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	}
	if len(value) == 0 || len(value)%aes.BlockSize != 0 {
		return nil, status_error.IMProtocolInvalid
	}
	result := make([]byte, len(value))
	for offset := 0; offset < len(value); offset += aes.BlockSize {
		if encrypt {
			block.Encrypt(result[offset:offset+aes.BlockSize], value[offset:offset+aes.BlockSize])
		} else {
			block.Decrypt(result[offset:offset+aes.BlockSize], value[offset:offset+aes.BlockSize])
		}
	}
	if !encrypt {
		padding := int(result[len(result)-1])
		if padding == 0 || padding > aes.BlockSize || padding > len(result) {
			return nil, status_error.IMProtocolInvalid
		}
		for _, value := range result[len(result)-padding:] {
			if int(value) != padding {
				return nil, status_error.IMProtocolInvalid
			}
		}
		result = result[:len(result)-padding]
	}
	return result, nil
}
