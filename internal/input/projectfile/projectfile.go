// Package projectfile owns the rules of a file that the owner keeps in a
// project: its media type, its processing state, and the reason the owner reads when its
// processing stopped. Storage keeps the file, its record, and its jobs.
package projectfile

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// Media is the media type of an upload: the declared type, or by the file
// name's extension when none is declared, or sniffed from its first bytes.
func Media(name, declared string, head func() []byte) string {
	media := "application/octet-stream"
	if parsed, _, err := mime.ParseMediaType(declared); err == nil {
		media = parsed
	}
	if media != "application/octet-stream" && media != "" {
		return media
	}
	if byExt, _, err := mime.ParseMediaType(mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))); err == nil && byExt != "" {
		return byExt
	}
	if b := head(); len(b) > 0 {
		if sniffed, _, err := mime.ParseMediaType(http.DetectContentType(b)); err == nil && sniffed != "application/octet-stream" {
			return sniffed
		}
	}
	return media
}

// Job is one processing job of a file or of a text derived from it.
type Job struct {
	ID, Stage, State, Code string
}

// State is the processing state of a file from its jobs, latest first:
// failed when a job stopped, extracted when its extraction finished and
// nothing is left, stored otherwise. job is the failed job, or the latest.
func State(jobs []Job) (status, job, reason string) {
	status = "stored"
	extracting, done := false, false
	for i, j := range jobs {
		if i == 0 {
			job = j.ID
		}
		if (j.State == "failed" || j.State == "blocked") && status != "failed" {
			status, job, reason = "failed", j.ID, Reason(j.Code)
		}
		if j.State != "done" {
			extracting = true
		}
		if strings.HasPrefix(j.Stage, "source.extract") && j.State == "done" {
			done = true
		}
	}
	if status != "failed" && done && !extracting {
		status = "extracted"
	}
	return status, job, reason
}

// Reason is what the owner reads when the processing of a file stopped.
func Reason(code string) string {
	switch code {
	case "file_parser_unsupported":
		return "此文件类型尚无文字抽取器；原件已保存，可打开或下载，安装相应处理器后在此重试"
	case "file_pdf_page_limit":
		return "PDF超过现有100页解析上限；原件保留，请拆分为每份不超过100页后上传"
	case "file_pdf_invalid":
		return "PDF没有可读页；原件保留，请检查文件完整性后重试"
	case "file_audio_duration_limit":
		return "音频超过现有4小时转录上限；原件保留，请拆分后上传"
	case "file_audio_probe_failed":
		return "无法识别音频时长；原件保留，请检查音频格式或ffprobe处理器后重试"
	case "file_text_too_large":
		return "抽取文字超过现有1MiB解析上限；原件保留，请分成较小文件后上传"
	case "file_text_invalid":
		return "文件不是有效UTF-8文字或正文为空；原件保留，请换成UTF-8文字文件后重试"
	case "provider_not_configured", "provider_unavailable":
		return "模型通道未配置或暂不可用；原件保留，通道恢复后重试"
	case "model_output_invalid":
		return "模型抽取结果格式不合格；原件保留，可在此重试"
	case "file_parse_failed":
		return "文件解析失败；原件保留，请检查文件完整性和解析器后重试"
	default:
		return "处理未完成（" + code + "）；原件保留，可在此重试"
	}
}
