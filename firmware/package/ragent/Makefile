include $(TOPDIR)/rules.mk

PKG_NAME:=ragent
PKG_VERSION:=1.0.0
PKG_RELEASE:=1

include $(INCLUDE_DIR)/package.mk

define Package/ragent
  SECTION:=net
  CATEGORY:=Network
  TITLE:=AX6000 control agent (PPPoE/terminal/app-filter/iOS)
  DEPENDS:=+nftables +tc +rpcd +iwinfo
  URL:=https://example.com/ax6000-fw
endef

define Package/ragent/description
  运行在红米 AX6000 上的控制代理：
  上网方式管理(PPPoE/DHCP/静态/无线中继)、终端管理与上下线提醒、
  基于 DPI 的 App 访问限制，并提供局域网与云端两套控制接口（供 iOS App 使用）。
endef

# 二进制由 build/build.sh 交叉编译后放入 files/，这里不再重复编译
define Build/Prepare
endef

define Build/Configure
endef

define Build/Compile
endef

define Package/ragent/install
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) ./files/ragentd $(1)/usr/sbin/ragentd
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/ragentd.init $(1)/etc/init.d/ragentd
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/ragent.config $(1)/etc/config/ragent
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) ./files/ragent-pair $(1)/usr/bin/ragent-pair
endef

define Package/ragent/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] || {
  /etc/init.d/ragentd enable
  /etc/init.d/ragentd start
}
exit 0
endef

$(eval $(call BuildPackage,ragent))
