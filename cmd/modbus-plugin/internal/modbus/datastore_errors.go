package modbus

import "errors"

// DataStore の操作で返すエラー
var (
	// ErrAreaNotFound は指定されたエリアが存在しない場合のエラー
	ErrAreaNotFound = errors.New("memory area not found")

	// ErrAddressOutOfRange はアドレスが範囲外の場合のエラー
	ErrAddressOutOfRange = errors.New("address out of range")

	// ErrReadOnly は読み取り専用エリアへの書き込み試行時のエラー
	ErrReadOnly = errors.New("area is read-only")

	// ErrTypeMismatch はエリアタイプと操作が一致しない場合のエラー
	ErrTypeMismatch = errors.New("type mismatch: bit operation on word area or vice versa")

	// ErrInvalidData はデータが無効な場合のエラー
	ErrInvalidData = errors.New("invalid data")
)
