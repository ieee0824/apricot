package compose

// ComposeFile represents a docker-compose.yaml file.
type ComposeFile struct {
	Services map[string]Service `yaml:"services"`
	Networks map[string]Network `yaml:"networks"`
	Volumes  map[string]Volume  `yaml:"volumes"`
}

// BuildConfig holds the resolved build configuration for a service.
type BuildConfig struct {
	Context    string
	Dockerfile string
	Args       map[string]string
	Target     string
	Labels     map[string]string
	NoCache    bool
}

// Service represents a service in docker-compose.yaml.
type Service struct {
	Image         string      `yaml:"image"`
	Build         interface{} `yaml:"build"` // string or map
	Command       interface{} `yaml:"command"`     // string or []string
	Entrypoint    interface{} `yaml:"entrypoint"`  // string or []string
	Environment   interface{} `yaml:"environment"` // map[string]string or []string
	EnvFile       interface{} `yaml:"env_file"`    // string or []string
	Ports         []string    `yaml:"ports"`
	Volumes       []string    `yaml:"volumes"`
	Networks      interface{} `yaml:"networks"`    // []string or map
	Labels        interface{} `yaml:"labels"`      // map[string]string or []string
	WorkingDir    string      `yaml:"working_dir"`
	User          string      `yaml:"user"`
	CPUs          float64     `yaml:"cpus"`
	MemLimit      string      `yaml:"mem_limit"`
	StdinOpen     bool        `yaml:"stdin_open"`
	Tty           bool        `yaml:"tty"`
	DependsOn     interface{} `yaml:"depends_on"` // []string or map
	ContainerName string      `yaml:"container_name"`
	ReadOnly      bool        `yaml:"read_only"`
	Tmpfs         interface{} `yaml:"tmpfs"`      // string or []string
	DNS           interface{} `yaml:"dns"`        // string or []string
	DNSSearch     interface{} `yaml:"dns_search"` // string or []string
	DNSOpt        interface{} `yaml:"dns_opt"`    // string or []string
	Init          bool        `yaml:"init"`
	Ulimits       interface{} `yaml:"ulimits"` // map[string]int or map[string]{soft,hard}
	Restart       string      `yaml:"restart"`
}

// Network represents a network in docker-compose.yaml.
type Network struct {
	Driver   string            `yaml:"driver"`
	Internal bool              `yaml:"internal"`
	Labels   map[string]string `yaml:"labels"`
	External bool              `yaml:"external"`
	Name     string            `yaml:"name"` // override network name when external
}

// Volume represents a volume in docker-compose.yaml.
type Volume struct {
	Driver string            `yaml:"driver"`
	Labels map[string]string `yaml:"labels"`
}
