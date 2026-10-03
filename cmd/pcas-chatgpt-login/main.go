// pcas-chatgpt-login signs in with ChatGPT on the computer that runs the
// browser and leaves a protected credentials.json to transfer to the PCAS host
// (docs/chatgpt-plan-auth.md). It needs no database, API key or Codex.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai/siwc"
)

func main() {
	prepareConsole()
	err := run()
	if err != nil {
		fmt.Println()
		fmt.Println("没有完成：", err)
	}
	// Keep the window open when started by double-click.
	fmt.Println()
	fmt.Print("按回车键退出。")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		os.Exit(1)
	}
}

func defaultDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "pcas-chatgpt")
	}
	return "pcas-chatgpt"
}

func run() error {
	dir := flag.String("dir", defaultDir(), "存放登录凭据的文件夹")
	port := flag.Int("port", 1455, "本机回调端口，要和服务器上的设置一致")
	client := flag.String("client", "", "重新授权时填上次签发的客户端 ID（oaiapp_ 开头）；首次登录留空")
	flag.Parse()

	fmt.Println("PCAS · 用 ChatGPT 套餐登录（本机授权助手）")
	fmt.Println()
	in := bufio.NewReader(os.Stdin)
	if *client == "" && flag.NFlag() == 0 {
		fmt.Println("第一次登录：直接按回车。")
		fmt.Print("重新授权：粘贴上次的客户端 ID（oaiapp_ 开头）后按回车：")
		line, _ := in.ReadString('\n')
		*client = strings.TrimSpace(line)
		fmt.Println()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	m, err := siwc.New(*dir, "127.0.0.1", *port)
	if err != nil {
		return err
	}
	defer m.Close()
	login, err := m.Begin(ctx, *client, false, false)
	if err != nil {
		if *client != "" {
			return fmt.Errorf("%w（重新授权需要这台电脑上次登录留下的凭据文件夹：%s）", err, *dir)
		}
		return err
	}
	fmt.Println("正在打开浏览器。如果没有自动打开，把下面这个地址复制到这台电脑的浏览器里：")
	fmt.Println()
	fmt.Println(login.AuthorizationURL)
	fmt.Println()
	fmt.Println("在浏览器里登录并同意之后回到这里。登录会使用你的 ChatGPT 套餐额度。")
	_ = openBrowser(login.AuthorizationURL)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("已取消")
		case <-ticker.C:
			status, err := m.Status(ctx)
			if err != nil {
				return err
			}
			if status.Pending {
				continue
			}
			if status.Error != "" {
				return fmt.Errorf("%s", status.Error)
			}
			for _, account := range status.Accounts {
				if account.ClientID != status.Active || !account.Connected {
					continue
				}
				file := filepath.Join(*dir, "credentials.json")
				fmt.Println()
				fmt.Println("登录成功。")
				fmt.Println("客户端 ID（重新授权时要用，请记下）：", account.ClientID)
				if !account.PlanEnabled {
					fmt.Println("注意：这次没有授予使用套餐的权限，登录了但不能用来生成。请重新运行并在授权页同意套餐权限。")
				}
				fmt.Println("凭据文件：", file)
				fmt.Println()
				fmt.Println("下一步：把这个文件通过 SSH 传到 PCAS 服务器，例如在这台电脑上运行：")
				fmt.Printf("  scp \"%s\" 用户名@服务器地址:/root/chatgpt-transfer.json\n", file)
				fmt.Println("传完之后在服务器上导入。导入完成前不要在这台电脑上退出登录，也不要再运行本程序去刷新；")
				fmt.Println("这个文件等同于你的登录凭据，不要通过聊天软件、网页或邮件发送，导入后请从这台电脑上删除。")
				return nil
			}
			return fmt.Errorf("没有完成 ChatGPT 登录")
		}
	}
}
