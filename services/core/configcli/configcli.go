// Package configcli — общая подкоманда "<служба> config": записывает конфиг службы
// ~/.cbr/config/<служба>.json. Работает в двух режимах:
//
//   - без флагов значений — спрашивает все параметры по очереди (Enter оставляет
//     текущее значение);
//   - с флагами — без вопросов задаёт переданные параметры, остальные оставляет
//     как в файле.
//
// Флаг --help печатает справку по параметрам. Состав параметров задаёт служба
// (её пакет internal/config), здесь только разбор флагов, диалог, справка и запись.
package configcli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	coreconfig "cbr/core/config"
)

// Name — имя подкоманды у бинарников служб.
const Name = "config"

// clearAnswer — ответ в диалоге, очищающий необязательный параметр.
const clearAnswer = "-"

// Param — один параметр конфига: флаг, вопрос в диалоге и строка справки.
type Param struct {
	// Flag — имя флага без дефисов: "base-url".
	Flag string
	// Key — ключ или секция в JSON: "base_url"; так параметр называется в диалоге и справке.
	Key string
	// Value — формат значения для справки: "<url>".
	Value string
	// Usage — описание одной строкой.
	Usage string
	// Required — пустое значение не принимается.
	Required bool
	// Secret — значение не печатается (токен).
	Secret bool
	// Get — текущее значение строкой; пусто — не задано.
	Get func() string
	// Set разбирает и сохраняет значение; пустая строка очищает необязательный параметр.
	Set func(string) error
}

// Command — подкоманда config одной службы.
type Command struct {
	// Service — имя службы и бинарника: "recorder".
	Service string
	// Path — файл конфига.
	Path string
	// Config — указатель на конфиг службы: в него читается файл, из него файл пишется.
	Config coreconfig.Validator
	// Params — параметры в порядке вопросов.
	Params []Param
	// Migrate переносит значения прежней нотации после чтения файла и возвращает
	// заметки для пользователя (необязательно).
	Migrate func() []string
	// Examples — примеры для справки, без имени бинарника: "config --cdn direct".
	Examples []string
}

// Run выполняет подкоманду с аргументами после слова config и возвращает код выхода:
// 0 — записано (или показана справка), 1 — конфиг не записан, 2 — ошибка в аргументах.
func (c *Command) Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(c.Service+" "+Name, flag.ContinueOnError)
	flags.SetOutput(io.Discard) // ошибки и справку печатаем сами
	values := make(map[string]*string, len(c.Params))
	for _, p := range c.Params {
		values[p.Flag] = flags.String(p.Flag, "", p.Usage)
	}
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) || (err == nil && flags.NArg() == 1 && flags.Arg(0) == "help") {
		c.help(stdout)
		return 0
	}
	if err == nil && flags.NArg() > 0 {
		err = fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if err != nil {
		return c.usageError(stderr, err)
	}

	notes, err := c.read()
	if err != nil {
		fmt.Fprintln(stderr, "❌", err)
		return 1
	}
	for _, note := range notes {
		fmt.Fprintln(stdout, "ℹ️ ", note)
	}

	given := 0
	flags.Visit(func(*flag.Flag) { given++ })
	if given == 0 {
		if err := c.ask(stdin, stdout); err != nil {
			fmt.Fprintln(stderr, "❌", err)
			return 1
		}
	} else {
		var errs []error
		flags.Visit(func(f *flag.Flag) {
			p := c.param(f.Name)
			if err := setParam(p, *values[f.Name]); err != nil {
				errs = append(errs, fmt.Errorf("--%s: %w", p.Flag, err))
			}
		})
		if err := errors.Join(errs...); err != nil {
			return c.usageError(stderr, err)
		}
	}

	c.Config.Normalize()
	if err := c.Config.Validate(); err != nil {
		fmt.Fprintf(stderr, "❌ %s is not saved, the config is invalid:\n%v\n", c.Path, err)
		return 1
	}
	if err := c.write(); err != nil {
		fmt.Fprintln(stderr, "❌", err)
		return 1
	}
	c.summary(stdout)
	return 0
}

func (c *Command) param(flagName string) Param {
	for _, p := range c.Params {
		if p.Flag == flagName {
			return p
		}
	}
	panic("configcli: unknown flag " + flagName)
}

func (c *Command) usageError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "❌ %v\nSee: %s %s --help\n", err, c.Service, Name)
	return 2
}

// read загружает существующий файл в Config без проверки: команда нужна в том
// числе для того, чтобы исправить конфиг, с которым служба не стартует.
func (c *Command) read() (notes []string, err error) {
	raw, err := os.ReadFile(c.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read config: %w", err)
	default:
		if err := json.Unmarshal(raw, c.Config); err != nil {
			return nil, fmt.Errorf("parse config %s: %w (fix or remove the file)", c.Path, err)
		}
	}
	c.Config.Normalize()
	if c.Migrate != nil {
		notes = c.Migrate()
	}
	return notes, nil
}

// setParam сохраняет значение с проверкой обязательности.
func setParam(p Param, value string) error {
	value = strings.TrimSpace(value)
	if value == "" && p.Required {
		return errors.New("a value is required")
	}
	return p.Set(value)
}

// ask спрашивает все параметры по очереди. Пустой ответ оставляет текущее значение,
// clearAnswer очищает необязательный параметр; неверный ответ переспрашивается.
func (c *Command) ask(stdin io.Reader, stdout io.Writer) error {
	fmt.Fprintf(stdout, "%s %s — %s\n", c.Service, Name, c.Path)
	fmt.Fprintf(stdout, "Enter keeps the value in [brackets], %q clears an optional setting, Ctrl+C cancels.\n\n", clearAnswer)
	in := bufio.NewReader(stdin)
	for _, p := range c.Params {
		for {
			current := p.Get()
			fmt.Fprintf(stdout, "%s — %s%s [%s]: ", p.Key, p.Usage, requiredMark(p), shown(p, current))
			line, err := in.ReadString('\n')
			if err != nil && (!errors.Is(err, io.EOF) || line == "") {
				fmt.Fprintln(stdout)
				return errors.New("input ended before all settings were answered, nothing is saved")
			}
			answer := strings.TrimSpace(line)
			switch {
			case answer == "" && current != "":
				// оставить как есть
			case answer == "" && !p.Required:
				// не задано и не нужно
			case answer == clearAnswer:
				err = setParam(p, "")
			default:
				err = setParam(p, answer)
			}
			if err == nil || errors.Is(err, io.EOF) {
				break
			}
			fmt.Fprintf(stdout, "  ❌ %v\n", err)
		}
	}
	fmt.Fprintln(stdout)
	return nil
}

func requiredMark(p Param) string {
	if p.Required {
		return " (required)"
	}
	return ""
}

// shown — значение для печати: секрет скрыт.
func shown(p Param, value string) string {
	switch {
	case value == "":
		return ""
	case p.Secret:
		return "set, hidden"
	default:
		return value
	}
}

// write записывает конфиг атомарно, с правами 0600 (в нём могут быть токены).
func (c *Command) write() error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // & в адресах — как есть
	enc.SetIndent("", "  ")
	if err := enc.Encode(c.Config); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(c.Path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // после успешного Rename файла уже нет
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp.Name(), c.Path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c *Command) keyWidth() int {
	width := 0
	for _, p := range c.Params {
		width = max(width, len(p.Key))
	}
	return width
}

func (c *Command) summary(stdout io.Writer) {
	fmt.Fprintf(stdout, "✅ Saved %s\n", c.Path)
	width := c.keyWidth()
	for _, p := range c.Params {
		value := shown(p, p.Get())
		if value == "" {
			value = "(not set)"
		}
		fmt.Fprintf(stdout, "   %-*s = %s\n", width, p.Key, value)
	}
	fmt.Fprintf(stdout, "Restart %s to apply.\n", c.Service)
}

func (c *Command) help(stdout io.Writer) {
	bin := c.Service + " " + Name
	fmt.Fprintf(stdout, `Writes the %s config: %s

Usage:
  %s            ask for every setting; Enter keeps the current value
  %s [flags]    set the given settings without questions, keep the rest as in the file
  %s --help     show this help

Settings:
`, c.Service, c.Path, bin, bin, bin)
	for _, p := range c.Params {
		fmt.Fprintf(stdout, "  --%s %s\n      %s: %s%s\n", p.Flag, p.Value, p.Key, p.Usage, requiredMark(p))
	}
	fmt.Fprintf(stdout, `
A connection setting takes %q, an HTTP proxy URL, or %q to switch back to the
proxy address saved in the file. An empty flag value (--flag "") clears an optional setting.
The config is checked before saving; an invalid one is not written.
Restart the service after changing its config.
`, coreconfig.ConnectionDirect, coreconfig.ConnectionProxy)
	if len(c.Examples) > 0 {
		fmt.Fprintln(stdout, "\nExamples:")
		for _, example := range c.Examples {
			fmt.Fprintf(stdout, "  %s %s\n", c.Service, example)
		}
	}
}

// --- готовые параметры ---------------------------------------------------------

// String — строковый параметр.
func String(flagName, key, value, usage string, required bool, target *string) Param {
	return Param{
		Flag: flagName, Key: key, Value: value, Usage: usage, Required: required,
		Get: func() string { return *target },
		Set: func(v string) error { *target = v; return nil },
	}
}

// Secret — строковый параметр, значение которого не печатается.
func Secret(flagName, key, value, usage string, required bool, target *string) Param {
	p := String(flagName, key, value, usage, required, target)
	p.Secret = true
	return p
}

// URL — абсолютный URL.
func URL(flagName, key, usage string, required bool, target *string) Param {
	p := String(flagName, key, "<url>", usage, required, target)
	p.Set = func(v string) error {
		if v != "" {
			if err := coreconfig.ValidateURL(v); err != nil {
				return err
			}
		}
		*target = v
		return nil
	}
	return p
}

// Int64 — целое число; 0 считается незаданным.
func Int64(flagName, key, value, usage string, required bool, target *int64) Param {
	return Param{
		Flag: flagName, Key: key, Value: value, Usage: usage, Required: required,
		Get: func() string {
			if *target == 0 {
				return ""
			}
			return strconv.FormatInt(*target, 10)
		},
		Set: func(v string) error {
			if v == "" {
				*target = 0
				return nil
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not a number", v)
			}
			*target = n
			return nil
		},
	}
}

// Connection — секция подключения (coreconfig.Connection) одним значением:
// "direct", адрес HTTP-прокси или "proxy" — снова включить сохранённый адрес.
// "direct" адрес в файле не стирает; пустое значение стирает и ставит direct.
func Connection(flagName, key, usage string, target *coreconfig.Connection) Param {
	return Param{
		Flag: flagName, Key: key, Usage: usage,
		Value: coreconfig.ConnectionDirect + "|" + coreconfig.ConnectionProxy + "|<proxy url>",
		Get: func() string {
			if target.Connection == coreconfig.ConnectionProxy && target.Proxy != "" {
				return target.Proxy
			}
			return coreconfig.ConnectionDirect
		},
		Set: func(v string) error {
			switch strings.ToLower(v) {
			case "":
				*target = coreconfig.Connection{Connection: coreconfig.ConnectionDirect}
			case coreconfig.ConnectionDirect:
				target.Connection = coreconfig.ConnectionDirect
			case coreconfig.ConnectionProxy:
				if target.Proxy == "" {
					return errors.New("no proxy address is saved yet: pass the proxy URL")
				}
				target.Connection = coreconfig.ConnectionProxy
			default:
				if err := coreconfig.ValidateURL(v); err != nil {
					return fmt.Errorf("want %q or a proxy URL: %w", coreconfig.ConnectionDirect, err)
				}
				*target = coreconfig.Connection{Connection: coreconfig.ConnectionProxy, Proxy: v}
			}
			return nil
		},
	}
}
