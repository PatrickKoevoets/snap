package control

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/intelsdilabs/gomit"

	"github.com/intelsdilabs/pulse/control/plugin"
	"github.com/intelsdilabs/pulse/core/control_event"
)

// control private key (RSA private key)
// control public key (RSA public key)
// Plugin token = token generated by plugin and passed to control
// Session token = plugin seed encrypted by control private key, verified by plugin using control public key
//

const (
	// LoadedPlugin States
	DetectedState pluginState = "detected"
	LoadingState  pluginState = "loading"
	LoadedState   pluginState = "loaded"
	UnloadedState pluginState = "unloaded"
)

type pluginState string

type pluginType int

type loadedPlugins []*LoadedPlugin

type executablePlugins []plugin.ExecutablePlugin

// Represents a plugin loaded or loading into control
type LoadedPlugin struct {
	Meta       plugin.PluginMeta
	Path       string
	Type       plugin.PluginType
	State      pluginState
	Token      string
	LoadedTime time.Time
}

type pluginControl struct {
	// TODO, going to need coordination on changing of these
	LoadedPlugins  loadedPlugins
	RunningPlugins executablePlugins
	Started        bool
	// loadRequestsChan chan LoadedPlugin

	controlPrivKey *rsa.PrivateKey
	controlPubKey  *rsa.PublicKey
	eventManager   *gomit.EventController
	subscriptions  *subscriptions
}

func (p *pluginControl) GenerateArgs(daemon bool) plugin.Arg {
	a := plugin.Arg{
		ControlPubKey: p.controlPubKey,
		PluginLogPath: "/tmp",
		RunAsDaemon:   daemon,
	}
	return a
}

func Control() *pluginControl {
	c := new(pluginControl)
	c.eventManager = new(gomit.EventController)
	c.subscriptions = new(subscriptions)
	c.subscriptions.Init()

	// c.loadRequestsChan = make(chan LoadedPlugin)
	// privatekey, err := rsa.GenerateKey(rand.Reader, 4096)

	// if err != nil {
	// 	panic(err)
	// }

	// // Future use for securing.
	// c.controlPrivKey = privatekey
	// c.controlPubKey = &privatekey.PublicKey

	return c
}

// Begin handling load, unload, and inventory
func (p *pluginControl) Start() {
	// begin controlling

	// Start load handler. We only start one to keep load requests handled in
	// a linear fashion for now as this is a low priority.
	// go p.HandleLoadRequests()

	p.Started = true
}

func (p *pluginControl) Stop() {
	// close(p.loadRequestsChan)
	p.Started = false
}

func (p *pluginControl) Load(path string) (*LoadedPlugin, error) {
	if !p.Started {
		return nil, errors.New("Must start plugin control before calling Load()")
	}

	/*
		Loading plugin status

		Before start (todo)
		* executable (caught on start)
		* signed? (todo)
		* Grab checksum (file watching? todo)
		=> Plugin state = detected

		After start before Ping
		* starts? (catch crash)
		* response? (catch stdout)
		=> Plugin state = loaded
	*/

	log.Printf("Attempting to load: %s\v", path)
	lPlugin := new(LoadedPlugin)
	lPlugin.Path = path
	lPlugin.State = DetectedState

	// Create a new Executable plugin
	//
	// In this case we only support Linux right now
	ePlugin, err := plugin.NewExecutablePlugin(p, lPlugin.Path, false)

	// If error then log and return
	if err != nil {
		log.Println(err)
		return nil, err
	}

	// Start the plugin using the start method
	err = ePlugin.Start()
	if err != nil {
		log.Println(err)
		return nil, err
	}

	var resp *plugin.Response
	// This blocks until a response or an error
	resp, err = plugin.WaitForResponse(ePlugin, time.Second*3)
	// resp, err = WaitForPluginResponse(ePlugin, time.Second*3)

	// If error then we log and return
	if err != nil {
		log.Println(err)
		return nil, err
	}

	// If the response state is not Success we log an error
	if resp.State != plugin.PluginSuccess {
		log.Printf("Plugin loading did not succeed: %s\n", resp.ErrorMessage)
		return nil, errors.New(fmt.Sprintf("Plugin loading did not succeed: %s\n", resp.ErrorMessage))
	}
	// On response we create a LoadedPlugin
	// and add to LoadedPlugins index
	//
	lPlugin.Meta = resp.Meta
	lPlugin.Type = resp.Type
	lPlugin.Token = resp.Token
	lPlugin.LoadedTime = time.Now()
	lPlugin.State = LoadedState

	p.LoadedPlugins = append(p.LoadedPlugins, lPlugin)

	/*

		Name
		Version
		Loaded Time

	*/

	return lPlugin, err
}

// subscribes a metric
func (p *pluginControl) SubscribeMetric(metric []string) {
	key := getMetricKey(metric)
	count := p.subscriptions.Subscribe(key)
	e := &control_event.MetricSubscriptionEvent{
		MetricNamespace: metric,
		Count:           count,
	}
	defer p.eventManager.Emit(e)
}

// unsubscribes a metric
func (p *pluginControl) UnsubscribeMetric(metric []string) {
	key := getMetricKey(metric)
	count, err := p.subscriptions.Unsubscribe(key)
	if err != nil {
		// panic because if a metric falls below 0, something bad has happened
		panic(err.Error())
	}
	e := &control_event.MetricUnsubscriptionEvent{
		MetricNamespace: metric,
		Count:           count,
	}
	defer p.eventManager.Emit(e)
}

func getMetricKey(metric []string) string {
	return strings.Join(metric, ".")
}
