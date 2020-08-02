package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"gopkg.in/yaml.v3"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	ColumnName = iota
	ColumnSize
	ColumnProgress
)

const appId = "com.github.gotk3.gotk3-examples.glade"

type OutFile struct {
	Name   string
	Iter   *gtk.TreeIter
	IsDone bool
}

var files = make([]OutFile, 0)

var win *gtk.ApplicationWindow
var treeStore *gtk.ListStore
var buttonConnect *gtk.Button
var buttonCancel *gtk.Button
var buttonSettings *gtk.Button
var headerBar *gtk.HeaderBar

var config Config

type Config struct {
	Client struct {
		Host string `yaml:"host"`
		Port string `yaml:"port"`
	} `yaml:"client"`
	Server struct {
		Listen    bool   `yaml:"listen"`
		Port      string `yaml:"port"`
		Directory string `yaml:"directory"`
	} `yaml:"server"`
}

func main() {
	loadConfig()

	// Create a new application.
	application, err := gtk.ApplicationNew(appId, glib.APPLICATION_FLAGS_NONE)
	failOnError(err)

	// Connect function to application startup event, this is not required.
	_, err = application.Connect("startup", func() {
		log.Println("application startup")
	})
	failOnError(err)

	// Connect function to application activate event
	_, err = application.Connect("activate", func() {
		log.Println("application activate")

		// Get the GtkBuilder UI definition in the glade file.
		builder, err := gtk.BuilderNewFromFile("ui/sfn-main.ui")
		failOnError(err)

		// Map the handlers to callback functions, and connect the signals
		// to the Builder.
		signals := map[string]interface{}{
			"on_main_window_destroy": onMainWindowDestroy,
		}
		builder.ConnectSignals(signals)

		// Get the object with the id of "main_window".
		obj, err := builder.GetObject("dialog_main")
		failOnError(err)

		// Verify that the object is a pointer to a gtk.ApplicationWindow.
		win, err = isApplicationWindow(obj)
		failOnError(err)

		obj, err = builder.GetObject("header")
		failOnError(err)
		headerBar, err = isHeader(obj)
		failOnError(err)

		obj, err = builder.GetObject("button_connect")
		failOnError(err)
		buttonConnect, err = isButton(obj)
		failOnError(err)

		obj, err = builder.GetObject("button_cancel")
		failOnError(err)
		buttonCancel, err = isButton(obj)
		failOnError(err)

		obj, err = builder.GetObject("button_import")
		failOnError(err)
		buttonImport, err := isButton(obj)
		failOnError(err)

		obj, err = builder.GetObject("button_settings")
		failOnError(err)
		buttonSettings, err = isButton(obj)
		failOnError(err)

		obj, err = builder.GetObject("tree_files")
		failOnError(err)
		tree, err := isTreeView(obj)
		failOnError(err)

		tree.AppendColumn(createTextColumn("File Name", ColumnName))
		tree.AppendColumn(createTextColumn("File Size", ColumnSize))
		tree.AppendColumn(createProgressColumn("Progress", ColumnProgress))

		// Creating a tree store. This is what holds the data that will be shown on our tree view.
		treeStore, err = gtk.ListStoreNew(glib.TYPE_STRING, glib.TYPE_STRING, glib.TYPE_INT)
		if err != nil {
			log.Fatal("Unable to create tree store:", err)
		}
		tree.SetModel(treeStore)

		_, err = buttonConnect.Connect("clicked", func() {
			builder, err := gtk.BuilderNewFromFile("ui/sfn-popover.ui")
			failOnError(err)

			obj, err = builder.GetObject("connect_popover")
			failOnError(err)
			popover, err := isPopover(obj)
			failOnError(err)

			obj, err = builder.GetObject("connect_host")
			failOnError(err)
			hostEntry, err := isEntry(obj)
			failOnError(err)
			hostEntry.SetText(config.Client.Host)

			obj, err = builder.GetObject("connect_port")
			failOnError(err)
			portEntry, err := isEntry(obj)
			failOnError(err)
			portEntry.SetText(config.Client.Port)

			obj, err = builder.GetObject("connect_button")
			failOnError(err)
			button, err := isButton(obj)
			failOnError(err)
			_, err = button.Connect("clicked", func() {
				host, err := hostEntry.GetText()
				failOnError(err)
				port, err := portEntry.GetText()
				failOnError(err)

				go func() {
					config.Client.Host = host
					config.Client.Port = port
					saveConfig()

					SwitchConnectionButton(true)
					err := RunClient(host, port)
					if err != nil {
						showError("Failed to connect to %s:%s", host, port)
					}
				}()
			})

			validateFunc := func() {
				log.Println("host/port changed")
				h, err := hostEntry.GetText()
				failOnError(err)
				p, err := portEntry.GetText()
				failOnError(err)
				button.SetSensitive(len(h) > 0 && len(p) > 0)
			}
			validateFunc()
			_, err = hostEntry.Connect("changed", validateFunc)
			failOnError(err)
			_, err = portEntry.Connect("changed", validateFunc)
			failOnError(err)

			popover.SetRelativeTo(buttonConnect)

			popover.Show()

		})
		failOnError(err)

		_, err = buttonCancel.Connect("clicked", func() {
			err := Disconnect()
			if err != nil {
				showError("Unable to disconnect")
				return
			}
			SetSubtitle("")
			SwitchConnectionButton(false)
			StartServerAsync()
		})

		_, err = buttonImport.Connect("clicked", func() {
			dialog, err := gtk.FileChooserNativeDialogNew(
				"Choose the file to send",
				win,
				gtk.FILE_CHOOSER_ACTION_OPEN,
				"Open",
				"Cancel",
			)
			failOnError(err)
			dialog.SetModal(true)
			dialog.SetSelectMultiple(true)
			v := dialog.Run()
			if v == int(gtk.RESPONSE_ACCEPT) {
				list, err := dialog.GetFilenames()
				if err != nil {
					log.Println("unable to choose files")
					return
				}
				for _, name := range list {
					log.Println("open:", name)
					base := filepath.Base(name)
					stat, err := os.Stat(name)
					if err != nil {
						log.Println("unable to get file info")
						return
					}
					iter := addRow(treeStore, base, ByteCountBinary(stat.Size()))
					files = append(files, OutFile{Name: name, Iter: iter, IsDone: false})
				}
			}
		})
		failOnError(err)

		_, err = buttonSettings.Connect("clicked", func() {
			builder, err := gtk.BuilderNewFromFile("ui/sfn-settings.ui")
			failOnError(err)

			obj, err = builder.GetObject("settings_popover")
			failOnError(err)
			popover, err := isPopover(obj)
			failOnError(err)

			obj, err = builder.GetObject("host_switch")
			failOnError(err)
			hostSwitch, err := isSwitch(obj)
			failOnError(err)
			hostSwitch.SetActive(config.Server.Listen)

			obj, err = builder.GetObject("host_port")
			failOnError(err)
			portEntry, err := isEntry(obj)
			failOnError(err)
			portEntry.SetText(config.Server.Port)
			portEntry.SetSensitive(config.Server.Listen)

			obj, err = builder.GetObject("incoming_dir")
			failOnError(err)
			dirEntry, err := isEntry(obj)
			failOnError(err)
			dirEntry.SetText(config.Server.Directory)

			obj, err = builder.GetObject("select_dir")
			failOnError(err)
			selectDirButton, err := isButton(obj)
			failOnError(err)

			_, err = selectDirButton.Connect("clicked", func() {
				dialog, err := gtk.FileChooserNativeDialogNew("Select incoming files directory", win, gtk.FILE_CHOOSER_ACTION_SELECT_FOLDER, "Select", "Cancel")
				failOnError(err)
				dialog.SetModal(true)
				v := dialog.Run()
				if v == int(gtk.RESPONSE_ACCEPT) {
					name := dialog.GetFilename()
					log.Println("select dir:", name)
					config.Server.Directory = name
					dirEntry.SetText(config.Server.Directory)
					go func() { saveConfig() }()
				}
			})

			_, err = hostSwitch.Connect("state-set", func() {
				active := hostSwitch.GetActive()
				portEntry.SetSensitive(active)
				if !active {
					go func() {
						time.Sleep(250 * time.Millisecond)
						popover.Hide()
					}()
				}
			})

			_, err = popover.Connect("closed", func() {
				log.Println("settings closed")
				l := hostSwitch.GetActive()
				p, err := portEntry.GetText()
				failOnError(err)

				dir, err := dirEntry.GetText()
				failOnError(err)
				config.Server.Directory = dir

				if l != config.Server.Listen || p != config.Server.Port {
					config.Server.Listen = l
					config.Server.Port = p
					go func() {
						StopServer()
						SwitchConnectionButton(false)
						if config.Server.Listen {
							StartServerAsync()
						}
					}()
				}
				go func() { saveConfig() }()
			})

			popover.SetRelativeTo(buttonSettings)

			popover.Show()
		})
		failOnError(err)

		// Show the Window and all of its components.
		win.Show()
		application.AddWindow(win)

		StartServerAsync()
	})
	failOnError(err)

	// Connect function to application shutdown event, this is not required.
	_, err = application.Connect("shutdown", func() {
		log.Println("application shutdown")
	})
	failOnError(err)

	// Launch the application
	os.Exit(application.Run(os.Args))
}

func loadConfig() {
	loaded := false
	f, err := os.Open("config.yml")
	if err == nil {
		//noinspection GoUnhandledErrorResult
		defer f.Close()

		var cfg Config
		decoder := yaml.NewDecoder(f)
		err = decoder.Decode(&cfg)
		if err == nil {
			config = cfg
			loaded = true
		}
	}
	if !loaded {
		ex, err := os.Executable()
		if err != nil {
			panic(err)
		}
		current := filepath.Dir(ex)

		config.Client.Host = ""
		config.Client.Port = "3214"
		config.Server.Listen = false
		config.Server.Port = "3214"
		config.Server.Directory = current
	}
}

func saveConfig() {
	f, err := os.Create("config.yml")
	if err != nil {
		log.Println("unable to create config file")
		return
	}
	//noinspection GoUnhandledErrorResult
	defer f.Close()

	encoder := yaml.NewEncoder(f)
	err = encoder.Encode(config)
	//noinspection GoUnhandledErrorResult
	defer encoder.Close()
	if err != nil {
		log.Println("unable to save config file")
		return
	}
}

func StartServerAsync() {
	go func() {
		log.Println("server start")
		for StartServer() {
			log.Println("restart server")
		}
		log.Println("server stopped")
		SetSubtitle("")
	}()
}

func StartServer() bool {
	if !config.Server.Listen {
		return false
	}
	ip, err := GetIpAddr()
	if err != nil {
		ip = "Listening on port " + config.Server.Port
	} else {
		ip = "Listening on " + ip + ":" + config.Server.Port
	}
	log.Println("server ip", ip)
	SetSubtitle(ip)
	ip, err = Listen(config.Server.Port)
	if err != nil {
		log.Println("listening failed")
		return false
	}
	ip = "Connected to " + ip
	SetSubtitle(ip)
	SwitchConnectionButton(true)
	ReceiveFiles()
	SendFiles()
	_ = Disconnect()
	StopServer()
	SwitchConnectionButton(false)
	return true
}

func StopServer() {
	err := Disconnect()
	if err != nil {
		log.Println("unable to disconnect")
	}
	err = StopListen()
	if err != nil {
		log.Println("unable to stop listening")
	}
}

func RunClient(host string, port string) error {
	StopServer()
	address := host + ":" + port
	log.Println("connect to", address)
	SetSubtitle("Connecting to " + address)
	ip, err := Connect(address)
	if err != nil {
		log.Println("unable to connect")
	} else {
		ip = "Connected to " + address
		SetSubtitle(ip)
		SendFiles()
		ReceiveFiles()
		_ = Disconnect()
	}
	SwitchConnectionButton(false)
	StartServerAsync()
	return err
}

func GetIpAddr() (string, error) {
	resp, err := http.Get("http://tomclaw.com/services/simple/getip.php")
	if err != nil {
		return "", err
	}
	//noinspection GoUnhandledErrorResult
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	line, _, err := reader.ReadLine()
	if err != nil {
		return "", err
	}
	return string(line), nil
}

func ReceiveFiles() {
	var iter *gtk.TreeIter
	for {
		more, err := ReadFile(config.Server.Directory, func(name string, size int64) {
			iter = addRow(treeStore, name, ByteCountBinary(size))
		}, func(p int) {
			err := treeStore.SetValue(iter, ColumnProgress, p)
			if err != nil {
				log.Fatal("unable set value:", err)
			}
		})
		if err != nil {
			showError("File receiving error")
			break
		}
		if !more {
			log.Println("done receiving files")
			break
		}
		log.Println("receive next file")
	}
}

func SendFiles() {
	var err error
	if len(files) > 0 {
		for _, outFile := range files {
			if outFile.IsDone {
				continue
			}
			err = SendFile(outFile.Name, func(p int) {
				err := treeStore.SetValue(outFile.Iter, ColumnProgress, p)
				if err != nil {
					log.Fatal("unable set value:", err)
				}
			})
			if err != nil {
				break
			}
			outFile.IsDone = true
		}
	}
	if err == nil {
		err = SendDone()
	}
	if err != nil {
		showError("File sending error")
	}
}

func SwitchConnectionButton(connected bool) {
	_, _ = glib.IdleAdd(buttonCancel.SetVisible, connected)
	_, _ = glib.IdleAdd(buttonConnect.SetVisible, !connected)
	_, _ = glib.IdleAdd(buttonSettings.SetVisible, !connected)
}

func SetSubtitle(subtitle string) {
	_, _ = glib.IdleAdd(headerBar.SetSubtitle, subtitle)
}

func isApplicationWindow(obj glib.IObject) (*gtk.ApplicationWindow, error) {
	// Make type assertion (as per gtk.go).
	if win, ok := obj.(*gtk.ApplicationWindow); ok {
		return win, nil
	}
	return nil, errors.New("not a *gtk.Window")
}

func isHeader(obj glib.IObject) (*gtk.HeaderBar, error) {
	// Make type assertion (as per gtk.go).
	if headerBar, ok := obj.(*gtk.HeaderBar); ok {
		return headerBar, nil
	}
	return nil, errors.New("not a *gtk.HeaderBar")
}

func isPopover(obj glib.IObject) (*gtk.Popover, error) {
	// Make type assertion (as per gtk.go).
	if popover, ok := obj.(*gtk.Popover); ok {
		return popover, nil
	}
	return nil, errors.New("not a *gtk.Popover")
}

func isSwitch(obj glib.IObject) (*gtk.Switch, error) {
	// Make type assertion (as per gtk.go).
	if switchWidget, ok := obj.(*gtk.Switch); ok {
		return switchWidget, nil
	}
	return nil, errors.New("not a *gtk.Switch")
}

func isEntry(obj glib.IObject) (*gtk.Entry, error) {
	// Make type assertion (as per gtk.go).
	if entry, ok := obj.(*gtk.Entry); ok {
		return entry, nil
	}
	return nil, errors.New("not a *gtk.Entry")
}

func isButton(obj glib.IObject) (*gtk.Button, error) {
	// Make type assertion (as per gtk.go).
	if button, ok := obj.(*gtk.Button); ok {
		return button, nil
	}
	return nil, errors.New("not a *gtk.Button")
}

func isTreeView(obj glib.IObject) (*gtk.TreeView, error) {
	// Make type assertion (as per gtk.go).
	if tree, ok := obj.(*gtk.TreeView); ok {
		return tree, nil
	}
	return nil, errors.New("not a *gtk.TreeView")
}

// Add a column to the tree view (during the initialization of the tree view)
// We need to distinct the type of data shown in either column.
func createTextColumn(title string, id int) *gtk.TreeViewColumn {
	cellRenderer, err := gtk.CellRendererTextNew()
	if err != nil {
		log.Fatal("Unable to create text cell renderer:", err)
	}

	column, err := gtk.TreeViewColumnNewWithAttribute(title, cellRenderer, "text", id)
	if err != nil {
		log.Fatal("Unable to create cell column:", err)
	}

	return column
}

// Add a column to the tree view (during the initialization of the tree view)
// We need to distinct the type of data shown in either column.
func createProgressColumn(title string, id int) *gtk.TreeViewColumn {
	// In this column we want to show text, hence create a text renderer
	cellRenderer, err := gtk.CellRendererProgressNew()
	if err != nil {
		log.Fatal("Unable to create progress cell renderer:", err)
	}

	// Tell the renderer where to pick input from. Text renderer understands
	// the "int" property.
	column, err := gtk.TreeViewColumnNewWithAttribute(title, cellRenderer, "value", id)
	if err != nil {
		log.Fatal("Unable to create cell column:", err)
	}

	return column
}

// Append a toplevel row to the tree store for the tree view
func addRow(treeStore *gtk.ListStore, name string, size string) *gtk.TreeIter {
	// Get an iterator for a new row at the end of the list store
	i := treeStore.Append()

	// Set the contents of the tree store row that the iterator represents
	err := treeStore.SetValue(i, ColumnName, name)
	if err != nil {
		log.Fatal("Unable set value:", err)
	}
	err = treeStore.SetValue(i, ColumnSize, size)
	if err != nil {
		log.Fatal("Unable set value:", err)
	}
	err = treeStore.SetValue(i, ColumnProgress, 0)
	if err != nil {
		log.Fatal("Unable set value:", err)
	}
	return i
}

func failOnError(e error) {
	if e != nil {
		// panic for any errors.
		log.Panic(e)
	}
}

func showError(format string, a ...interface{}) {
	_, _ = glib.IdleAdd(func() {
		dialog := gtk.MessageDialogNew(win, gtk.DIALOG_MODAL, gtk.MESSAGE_ERROR, gtk.BUTTONS_CLOSE, format, a...)
		_, err := dialog.Connect("response", func() {
			dialog.Hide()
		})
		failOnError(err)
		dialog.Show()
	})
}

// onMainWindowDestroy is the callback that is linked to the
// on_main_window_destroy handler. It is not required to map this,
// and is here to simply demo how to hook-up custom callbacks.
func onMainWindowDestroy() {
	log.Println("onMainWindowDestroy")
}

func ByteCountBinary(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
