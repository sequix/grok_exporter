grok_exporter mod
=============

修改原始 [grok_exporter](https://github.com/fstab/grok_exporter)，支持软链接、日志偏移记录。

## 使用

```shell
# 先下载用到的pattern文件
git submodule update --init --recursive

# 运行
go run grok_exporter.go -config config.yml
```

```
# 偏移文格式如下：
{
    "<deviceNumberHex>-<inodeNumberHex>": <offsetDec>,
    "10302-642271": 941183,
    "10302-64227b": 627455
}
```

配置文件见config.yml。

## 打包镜像

```bash
# 1.修改release.sh和Dockerfile中的版本号

# 2.编译
$ ./release.sh linux-amd64

# 3.在github.com上发布版本

# 4.打包
$ docker build .
```

## 实现

![implementation diagram](images/grok.jpg)

删除tailer/fswatcher中原有内容，只保留对外interface，该接口对外暴漏两个chan，发送日志行的Lines 和 发送Error的errors

watcher、poller实现fswatcher.Interface，提供以mixed和轮询两种形式的日志抓取方式。

* mixed：通过inotify监控文件夹事件，通过轮询日志文件抓取日志。保证不会掉日志。
* 轮询：以一定间隔做以下动作：stopAllFiles, listdirs, startAllFiles。不掉日志、兼容性好，但占用fd、mem高。

初始化watcher、poller时注入position.Interface，保存文件偏移的三元组<dev,ino,offset>，并周期性同步到磁盘

watcher使用 [fsnotify](https://github.com/fsnotify/fsnotify) 监听文件夹、[hpcloud/tail](https://github.com/hpcloud/tail) 轮询文件，文件有变化时，tailer将新内容发送到Lines

poller周期性list文件夹，对每个匹配的日志文件开一个goroutine (file)读取日志行，并发送到Lines

tailer和file实现均使用 [Fan-In](https://github.com/tmrts/go-patterns/blob/master/messaging/fan_in.md) 模式