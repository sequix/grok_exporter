grok_exporter mod
=============

修改原始 [grok_exporter](https://github.com/fstab/grok_exporter)，支持软链接、日志偏移记录。

## 使用

```shell
# 先下载用到的pattern文件
cd ./logstash-patterns-core
git clone https://github.com/logstash-plugins/logstash-patterns-core.git
mv logstash-patterns-core/patterns .
rm -r logstash-patterns-core

# 运行
go run grok_exporter.go -config config.yml
```

```yaml
global:
    config_version: 2
# 新增position配置项，记录日志读取的偏移
position:
    # 偏移文件路径
    position_file: ./position.json
    # 偏移文件同步周期
    sync_interval: 5s
input:
    type: file
    path: ./dir/*.log
    readall: true
    #poll_interval_seconds: 3
grok:
    patterns_dir: ./logstash-patterns-core/patterns
    additional_patterns:
    - 'EXIM_MESSAGE [a-zA-Z ]*'
metrics:
    - type: counter
      name: exim_rejected_rcpt_total
      help: Total number of rejected recipients, partitioned by error message.
      match: '%{EXIM_DATE} %{EXIM_REMOTE_HOST} F=<%{EMAILADDRESS}> rejected RCPT <%{EMAILADDRESS}>: %{EXIM_MESSAGE:message}'
      labels:
          error_message: '{{.message}}'
server:
    host: localhost
    port: 8989
```

```
# 偏移文件以json保存日志文件的inode编号和偏移量

{
    "4637271": 3213,
    "6562434": 2650
}
```

## 实现

![implementation diagram](images/grok.jpg)

删除tailer/fswatcher中原有内容，只保留对外interface，该接口对外暴漏两个chan，发送日志行的Lines 和 发送Error的errors

watcher、poller实现fswatcher.Interface，提供以fsnotify和轮询两种形式的日志抓取方式

初始化watcher、poller时注入position.Interface，其本质是一个 map[inode_number]offset，其内容会周期性同步到磁盘

watcher使用 [fsnotify](https://github.com/fsnotify/fsnotify) 监听文件夹、[hpcloud/tail](https://github.com/hpcloud/tail) 监听文件，文件有变化时，将新内容发送到Lines

poller周期性list文件夹，对每个匹配的日志文件开一个goroutine读取日志行，并发送到Lines

watcher和poller实现均使用 [Fan-In](https://github.com/tmrts/go-patterns/blob/master/messaging/fan_in.md) 模式