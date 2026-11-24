package blobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"strings"
)

const maxAvatarBytes = 2 << 20

type AvatarUpload struct {
	Avatar      string `json:"avatar"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
}

func UploadAvatar(admits *platform.Error, files platform.FileStore, tenant string, body io.Reader) (AvatarUpload, *platform.Error) {
	if admits != nil {
		return AvatarUpload{}, admits
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxAvatarBytes+1))
	if err != nil {
		return AvatarUpload{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Avatar image could not be read")
	}
	if len(raw) == 0 || len(raw) > maxAvatarBytes {
		return AvatarUpload{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Avatar image must be under 2 MiB")
	}
	contentType := detectAvatarType(raw)
	if contentType == "" {
		return AvatarUpload{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an AVIF, WebP, PNG or JPEG image")
	}
	if contentType != "image/avif" {
		if converted, ok := transcodeAvatarAVIF(raw); ok {
			raw = converted
			contentType = "image/avif"
		}
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	key := tenant + "/avatars/" + hash
	if err := files.Put(context.Background(), key, raw, contentType); err != nil {
		return AvatarUpload{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Avatar could not be stored")
	}
	return AvatarUpload{Avatar: "sha256:" + hash, ContentType: contentType, Size: len(raw)}, nil
}

func ValidAvatarHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func StreamAvatar(w http.ResponseWriter, files platform.FileStore, tenant, hash string) {
	body, _, err := files.Get(context.Background(), tenant+"/avatars/"+hash)
	if err != nil {
		http.NotFound(w, &http.Request{})
		return
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxAvatarBytes+1))
	if err != nil {
		http.Error(w, "avatar unavailable", http.StatusBadGateway)
		return
	}
	contentType := detectAvatarType(data)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func DownloadAvatar(w http.ResponseWriter, admits *platform.Error, files platform.FileStore, tenant, hash string, outside func(http.ResponseWriter, func(http.ResponseWriter))) *platform.Error {
	if admits != nil {
		return admits
	}
	if !ValidAvatarHash(hash) {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	outside(w, func(w http.ResponseWriter) { StreamAvatar(w, files, tenant, hash) })
	return nil
}

func detectAvatarType(data []byte) string {
	if len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) {
		brand := string(data[8:12])
		if brand == "avif" || brand == "avis" || brand == "mif1" {
			return "image/avif"
		}
	}
	if len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return "image/webp"
	}
	if len(data) >= 8 && bytes.Equal(data[0:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	return ""
}

func transcodeAvatarAVIF(raw []byte) ([]byte, bool) {
	magick := ""
	for _, candidate := range []string{"magick", "/opt/homebrew/bin/magick", "/usr/local/bin/magick"} {
		if path, err := exec.LookPath(candidate); err == nil {
			magick = path
			break
		}
	}
	if magick == "" {
		return nil, false
	}
	dir, err := os.MkdirTemp("", "platform-avatar-*")
	if err != nil {
		return nil, false
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "in")
	dst := filepath.Join(dir, "out.avif")
	if os.WriteFile(src, raw, 0o600) != nil {
		return nil, false
	}
	cmd := exec.Command(magick, src, "-auto-orient", "-thumbnail", "256x256^", "-gravity", "center", "-extent", "256x256", "-strip", "-quality", "76", dst)
	if cmd.Run() != nil {
		return nil, false
	}
	out, err := os.ReadFile(dst)
	if err != nil || len(out) == 0 || !strings.HasPrefix(detectAvatarType(out), "image/avif") {
		return nil, false
	}
	return out, true
}
