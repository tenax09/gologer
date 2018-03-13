package go_logger

import (
	"fmt"
	"sync"
	"runtime"
	"path"
	"time"
	"os"
	"strconv"
)

const (
	LOGGER_LEVEL_EMERGENCY = iota
	LOGGER_LEVEL_ALERT
	LOGGER_LEVEL_CRITICAL
	LOGGER_LEVEL_ERROR
	LOGGER_LEVEL_WARNING
	LOGGER_LEVEL_NOTICE
	LOGGER_LEVEL_INFO
	LOGGER_LEVEL_DEBUG
)

type adapterLoggerFunc func() LoggerAbstract

type LoggerAbstract interface {
	Name() string
	Init(config *Config) error
	Write(loggerMsg *loggerMessage) error
	Flush()
}

var adapters = make(map[string]adapterLoggerFunc)

var levelStringMapping = map[int]string{
	LOGGER_LEVEL_EMERGENCY:   "Emergency",
	LOGGER_LEVEL_ALERT:       "Alert",
	LOGGER_LEVEL_CRITICAL:    "Critical",
	LOGGER_LEVEL_ERROR:       "Error",
	LOGGER_LEVEL_WARNING:     "Warning",
	LOGGER_LEVEL_NOTICE:      "Notice",
	LOGGER_LEVEL_INFO:        "Info",
	LOGGER_LEVEL_DEBUG:       "Debug",
}

//Register logger adapter
func Register(adapterName string, newLog adapterLoggerFunc)  {
	if adapters[adapterName] != nil {
		panic("logger: logger adapter "+ adapterName +" already registered!")
	}
	if newLog == nil {
		panic("logger: looger adapter "+ adapterName +" is nil!")
	}

	adapters[adapterName] = newLog
}

type Logger struct {
	level       int //日志级别
	lock        sync.Mutex //互斥锁
	outputs     []*outputLogger //输出 loggers
	msgChan     chan *loggerMessage // message channel 通道
	synchronous bool //同步
	wait        sync.WaitGroup //线程阻塞
	signalChan  chan string //信号 channel
}

type outputLogger struct {
	Name string
	LoggerAbstract
}

type loggerMessage struct {
	Timestamp int64 `json:"timestamp"`
	TimestampFormat string `json:"timestamp_format"`
	Millisecond int64 `json:"millisecond"`
	MillisecondFormat string `json:"millisecond_format"`
	Level int `json:"level"`
	LevelString string `json:"level_string"`
	Body string `json:"body"`
	File string `json:"file"`
	Line int `json:"line"`
	Function string `json:"function"`
}

//new logger
//return logger
func NewLogger() *Logger {
	logger := &Logger{
		level:          LOGGER_LEVEL_DEBUG,
		outputs:        []*outputLogger{},
		msgChan:        make(chan *loggerMessage, 10),
		synchronous:    true,
		wait:           sync.WaitGroup{},
		signalChan:     make(chan string, 1),
	}
	//default adapter console
	logger.attach("console", NewConfigConsole(&ConsoleConfig{}))

	return logger
}

//start attach a logger adapter
//param : adapterName console | file | database | ...
//return : error
func (logger *Logger) Attach(adapterName string, config *Config) error {
	logger.lock.Lock()
	defer logger.lock.Unlock()

	return logger.attach(adapterName, config)
}

//attach a logger adapter after lock
//param : adapterName console | file | database | ...
//return : error
func (logger *Logger) attach(adapterName string, config *Config) error {
	for _, output := range logger.outputs {
		if(output.Name == adapterName) {
			printError("logger: adapter " +adapterName+ "already attached!")
		}
	}
	logFun, ok := adapters[adapterName]
	if !ok {
		printError("logger: adapter " +adapterName+ "is nil!")
	}
	adapterLog := logFun()
	err := adapterLog.Init(config)
	if err != nil {
		printError("logger: adapter " +adapterName+ " init failed, error: " +err.Error())
	}

	output := &outputLogger {
		Name:adapterName,
		LoggerAbstract: adapterLog,
	}

	logger.outputs = append(logger.outputs, output)
	return nil
}

//start attach a logger adapter
//param : adapterName console | file | database | ...
//return : error
func (logger *Logger) Detach(adapterName string) error {
	logger.lock.Lock()
	defer logger.lock.Unlock()

	return logger.detach(adapterName)
}

//detach a logger adapter after lock
//param : adapterName console | file | database | ...
//return : error
func (logger *Logger) detach(adapterName string) error {
	outputs := []*outputLogger{}
	for _, output := range logger.outputs {
		if(output.Name == adapterName) {
			continue
		}
		outputs = append(outputs, output)
	}
	logger.outputs = outputs
	return nil
}

//set logger level
//params : level int
func (logger *Logger) SetLevel(level int) {
	logger.level = level
}

//set logger synchronous false
//params : sync bool
func (logger *Logger) SetAsync(data... int) {
	logger.lock.Lock()
	defer logger.lock.Unlock()
	logger.synchronous = false

	msgChanLen := 100
	if(len(data) > 0) {
		msgChanLen = data[0]
	}

	logger.msgChan = make(chan *loggerMessage, msgChanLen)
	logger.signalChan = make(chan string, 1)

	if (!logger.synchronous) {
		go func() {
			defer func() {
				e := recover()
				if e != nil {
					fmt.Printf("%v", e)
				}
			}()
			logger.startAsyncWrite()
		}()
	}
}

//write log message
//params : level int, msg string
//return : error
func (logger *Logger) Writer(level int, msg string) error {
	funcName := "null"
	pc, file, line, ok := runtime.Caller(2)
	if !ok {
		file = "null"
		line = 0
	}else {
		fun := runtime.FuncForPC(pc)
		funcName = fun.Name()
	}
	_, filename := path.Split(file)

	levelString := levelStringMapping[level]
	if(levelString == "") {
		printError("logger: level " + strconv.Itoa(level) + " is illegal!")
	}

	loggerMsg := &loggerMessage {
		Timestamp : time.Now().Unix(),
		TimestampFormat : time.Now().Format("2006-01-02 15:04:05"),
		Millisecond : time.Now().UnixNano()/1e6,
		MillisecondFormat : time.Now().Format("2006-01-02 15:04:05.999"),
		Level :level,
		LevelString: levelString,
		Body: msg,
		File : filename,
		Line : line,
		Function: funcName,
	}

	if(!logger.synchronous) {
		logger.wait.Add(1)
		logger.msgChan <- loggerMsg
	}else {
		logger.writeToOutputs(loggerMsg)
	}

	return nil
}

//sync write message to loggerOutputs
//params : loggerMessage
func (logger *Logger) writeToOutputs(loggerMsg *loggerMessage)  {
	for _, loggerOutput := range logger.outputs {
		err := loggerOutput.Write(loggerMsg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "logger: unable write loggerMessage to adapter:%v, error: %v\n", loggerOutput.Name, err)
		}
	}
}

//start async write by read logger.msgChan
func (logger *Logger) startAsyncWrite()  {
	for {
		select {
		case loggerMsg := <-logger.msgChan:
			logger.writeToOutputs(loggerMsg)
			logger.wait.Done()
		case signal := <-logger.signalChan:
			if signal == "flush" {
				logger.flush()
			}
		}
	}
}

//flush msgChan data
func (logger *Logger) flush() {
	if !logger.synchronous {
		for {
			if len(logger.msgChan) > 0 {
				loggerMsg := <-logger.msgChan
				logger.writeToOutputs(loggerMsg)
				logger.wait.Done()
				continue
			}
			break
		}
		for _, loggerOutput := range logger.outputs {
			loggerOutput.Flush()
		}
	}
}

//if SetAsync() or logger.synchronous is false, must call Flush() to flush msgChan data
func (logger *Logger) Flush()  {
	if !logger.synchronous {
		logger.signalChan <- "flush"
		logger.wait.Wait()
		return
	}
	logger.flush()
}

//log emergency level
func (logger *Logger) Emergency(msg string) {
	if logger.level < LOGGER_LEVEL_EMERGENCY {
		return
	}

	logger.Writer(LOGGER_LEVEL_EMERGENCY, msg)
}

//log emergency format
func (logger *Logger) Emergencyf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	logger.Emergency(msg)
}

//log alert level
func (logger *Logger) Alert(msg string) {
	if logger.level < LOGGER_LEVEL_ALERT {
		return
	}

	logger.Writer(LOGGER_LEVEL_ALERT, msg)
}

//log alert format
func (logger *Logger) Alertf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	logger.Alert(msg)
}

//log critical level
func (logger *Logger) Critical(msg string) {
	if logger.level < LOGGER_LEVEL_CRITICAL {
		return
	}

	logger.Writer(LOGGER_LEVEL_CRITICAL, msg)
}

//log critical format
func (logger *Logger) Criticalf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	logger.Critical(msg)
}

//log error level
func (logger *Logger) Error(msg string) {
	if logger.level < LOGGER_LEVEL_ERROR {
		return
	}

	logger.Writer(LOGGER_LEVEL_ERROR, msg)
}

//log error format
func (logger *Logger) Errorf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	logger.Error(msg)
}

//log warning level
func (logger *Logger) Warning(msg string) {
	if logger.level < LOGGER_LEVEL_WARNING {
		return
	}

	logger.Writer(LOGGER_LEVEL_WARNING, msg)
}

//log warning format
func (logger *Logger) Warningf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	logger.Warning(msg)
}

//log notice level
func (logger *Logger) Notice(msg string) {
	if logger.level < LOGGER_LEVEL_NOTICE {
		return
	}

	logger.Writer(LOGGER_LEVEL_NOTICE, msg)
}

//log notice format
func (logger *Logger) Noticef(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	logger.Notice(msg)
}

//log info level
func (logger *Logger) Info(msg string) {
	if logger.level < LOGGER_LEVEL_INFO {
		return
	}

	logger.Writer(LOGGER_LEVEL_INFO, msg)
}

//log info format
func (logger *Logger) Infof(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	logger.Info(msg)
}

//log debug level
func (logger *Logger) Debug(msg string) {
	if logger.level < LOGGER_LEVEL_DEBUG {
		return
	}

	logger.Writer(LOGGER_LEVEL_DEBUG, msg)
}

//log debug format
func (logger *Logger) Debugf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	logger.Debug(msg)
}

func printError(message string) {
	fmt.Println(message)
	os.Exit(0)
}