# 红米 AX6000 刷机与部署

> **风险提示**：刷机会清除原厂系统、影响保修，操作不当可能变砖。请先完整备份，并确认你理解每一步。
> 硬件版本不同（AX6000 存在不同批次），请先在恩山/社区确认你的机器对应的固件与分区布局。

## 0. 准备

- 电脑一台 + 网线，Windows 需准备 TFTP 工具（如 tftpd64）与串口工具（可选）
- 固件镜像：`dist/firmware/*sysupgrade.itb`（编译产物）
- OpenWrt/ImmortalWrt 的 U-Boot（ubootmod 版，用于 AX6000）
- 备份工具：原厂分区可通过 `dd` 备份到 U 盘或 `scp` 到本地

## 1. 备份原厂分区

```sh
# 通过 SSH 进入原厂系统后（见第 2 步）
for mtd in $(ls /dev/mtd* | grep -E 'mtd[0-9]+$'); do
  dd if=$mtd of=/tmp/$(basename $mtd).bin
done
# 用 scp 拷到电脑保存
```

## 2. 获取 SSH 权限

社区通行做法：**用小米官方路由器修复工具刷回一个可以开启 SSH 的旧版官方固件**，再按该版本对应的官方/社区流程开启 SSH（不同版本方法不同，请以当前社区教程为准）。

关键点是：
- 修复工具需要在 Windows 上运行，按住 Reset 通电进入修复模式；
- 降级后不要联网自动升级，先把 SSH 打开；
- 拿到 SSH 后立即备份（第 1 步）。

> 本仓库不提供任何绕过设备安全机制的工具或脚本，请自行查阅社区公开教程。

## 3. 刷入 OpenWrt U-Boot

```sh
# 在官方系统 SSH 中执行（以 ubootmod 版本的 AX6000 U-Boot 为例）
mtd write /tmp/openwrt-mediatek-filogic-xiaomi_redmi-router-ax6000-ubootmod-squashfs-bl31-uboot.fip /dev/mtdX
```
其中 `mtdX` 为存放 U-Boot 的分区（**务必以你的机型文档为准，写错分区会变砖**）。

## 4. 刷入系统

断电 → 按住 Reset → 通电 → 大约 5~10 秒后松开，电脑网线接到 LAN 口，设置本机 IP 为 `192.168.1.2/24`，浏览器访问 `http://192.168.1.1` 进入 U-Boot 的 Web 恢复页，上传 `*-squashfs-sysupgrade.itb` 刷入。

## 5. 首次启动后的配置

```sh
uci set ragent.main.name='家里的路由器'
uci set ragent.cloud.enabled='1'
uci set ragent.cloud.url='wss://cloud.example.com/device'
uci set ragent.cloud.token='<云端签发的设备令牌>'
uci commit ragent
/etc/init.d/ragentd restart

# 生成配对二维码（浏览器访问 http://192.168.1.1/ragent-pair.png）
ragent-pair
```

## 6. 救砖

- **U-Boot 未损坏**：Reset 通电进 Web 恢复页重刷；
- **U-Boot 损坏**：需要拆机接 UART（波特率 115200）通过 TTL 刷写，或使用编程器刷写 SPI-NAND；
- **官方修复工具**在某些情况下仍可救回，可优先尝试。

## 7. 验证

```sh
# 代理运行
logread | grep ragentd

# 接口自检（把 <secret> 换成手机上配对到的值）
curl -s -X POST http://192.168.1.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"device_id":"<device_id>","secret":"<secret>","client":"web"}'
```
