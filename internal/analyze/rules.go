package analyze

// Rule tables. To add a category or signal, edit only this file.
//
// Signal kinds:
//   Deps  : package names found in manifest files (whole token match)
//   Files : substring of a lowercase file path ("suffix:" prefix = ends with)
//   Langs : GitHub language name (counts when at least 15% of the repo)
//   Words : words in description, topics or README
//
// Words include Turkish terms on purpose so that Turkish READMEs are matched too.

type Category struct {
	Name  string
	Deps  []string
	Files []string
	Langs []string
	Words []string
}

var Purposes = []Category{
	{
		Name:  "Web Frontend",
		Deps:  []string{"react", "react-dom", "vue", "@angular/core", "svelte", "next", "nuxt", "vite", "tailwindcss", "jquery", "bootstrap", "@sveltejs/kit", "astro", "solid-js", "remix"},
		Files: []string{"angular.json", "vite.config", "next.config", "nuxt.config", "svelte.config", "webpack.config"},
		Langs: []string{"Vue", "Svelte", "Astro"},
		Words: []string{"frontend", "front-end", "website", "web app", "landing page", "spa", "web sitesi"},
	},
	{
		Name:  "Web Backend / API",
		Deps:  []string{"express", "fastify", "@nestjs/core", "koa", "django", "flask", "fastapi", "spring-boot-starter-web", "spring-boot-starter", "gin-gonic/gin", "labstack/echo", "gofiber/fiber", "laravel/framework", "rails", "microsoft.aspnetcore", "actix-web", "axum", "rocket", "sequelize", "prisma", "typeorm", "sqlalchemy", "mongoose", "graphql", "grpc"},
		Files: []string{"routes/", "controllers/", "openapi.y", "swagger.", "migrations/"},
		Words: []string{"rest api", "backend", "back-end", "microservice", "graphql", "api server", "web service", "crud"},
	},
	{
		Name:  "Mobile Apps",
		Deps:  []string{"react-native", "expo", "flutter", "com.android.application", "androidx", "@ionic/angular", "@capacitor/core", "kotlinx-coroutines-android", "swiftui"},
		Files: []string{"androidmanifest.xml", "pubspec.yaml", "podfile", ".xcodeproj", "info.plist"},
		Langs: []string{"Dart", "Swift", "Objective-C"},
		Words: []string{"android", "ios app", "mobile app", "mobil uygulama", "flutter", "react native"},
	},
	{
		Name:  "Data Science / AI",
		Deps:  []string{"torch", "pytorch", "tensorflow", "keras", "scikit-learn", "sklearn", "pandas", "numpy", "transformers", "xgboost", "lightgbm", "opencv-python", "matplotlib", "seaborn", "langchain", "openai", "anthropic", "jax", "huggingface_hub", "ultralytics", "spacy", "nltk"},
		Files: []string{".ipynb", "notebooks/"},
		Langs: []string{"Jupyter Notebook", "R"},
		Words: []string{"machine learning", "deep learning", "neural network", "dataset", "data analysis", "nlp", "computer vision", "llm", "makine öğrenmesi", "yapay zeka", "veri analizi", "classification", "regression", "prediction"},
	},
	{
		Name:  "Embedded / IoT",
		Deps:  []string{"platformio", "arduino", "esp-idf", "freertos", "zephyr", "stm32", "mbed", "micropython", "paho-mqtt", "rpi.gpio", "pico-sdk"},
		Files: []string{"platformio.ini", "suffix:.ino", "suffix:.ioc", "sdkconfig", "linker.ld", "suffix:.ld", "startup_", "hal_conf", "cmsis", "kconfig", "suffix:.hex", "mcuboot", "freertos", "nortos", "suffix:.uf2", "suffix:.elf"},
		Langs: []string{"C", "Assembly"},
		Words: []string{"embedded", "firmware", "microcontroller", "arduino", "esp32", "esp8266", "stm32", "raspberry pi", "iot", "sensor", "mqtt", "rtos", "gömülü", "mikrodenetleyici", "bare-metal", "fw", "ble", "bluetooth low energy", "nrf52", "zigbee", "lora", "bootloader"},
	},
	{
		Name:  "Hardware / Electronics Design",
		Files: []string{"suffix:.kicad_pcb", "suffix:.kicad_sch", "suffix:.schdoc", "suffix:.pcbdoc", "suffix:.prjpcb", "suffix:.brd", "suffix:.sch", "gerber", "suffix:.xdc", "suffix:.qsf"},
		Langs: []string{"VHDL", "Verilog", "SystemVerilog", "KiCad Layout"},
		Words: []string{"pcb", "schematic", "fpga", "altium", "kicad", "hardware design", "circuit", "devre", "şematik"},
	},
	{
		Name:  "DevOps / Cloud",
		Deps:  []string{"boto3", "aws-sdk", "@aws-sdk/client-s3", "pulumi", "kubernetes", "docker", "ansible"},
		Files: []string{"dockerfile", "docker-compose", "suffix:.tf", "chart.yaml", "k8s/", "kubernetes/", "helm/", "ansible", "jenkinsfile", ".gitlab-ci.yml", "terraform"},
		Langs: []string{"HCL", "Dockerfile", "Nix"},
		Words: []string{"devops", "ci/cd", "kubernetes", "docker", "terraform", "infrastructure", "deployment", "cloud", "aws", "azure", "gcp", "monitoring"},
	},
	{
		Name:  "Game Development",
		Deps:  []string{"pygame", "phaser", "three", "babylonjs", "bevy", "monogame", "raylib", "sfml", "godot", "love2d"},
		Files: []string{"projectsettings/", "assets/scenes", "suffix:.unity", "suffix:.uproject", "project.godot", "suffix:.gd"},
		Langs: []string{"ShaderLab", "GDScript", "HLSL", "GLSL"},
		Words: []string{"game", "unity", "unreal", "godot", "oyun", "gameplay", "multiplayer"},
	},
	{
		Name:  "Desktop Apps",
		Deps:  []string{"electron", "tauri", "@tauri-apps/api", "@tauri-apps/cli", "pyqt5", "pyqt6", "pyside6", "tkinter", "wxpython", "avalonia", "winui", "fyne.io/fyne", "wails"},
		Files: []string{"suffix:.xaml", "suffix:.ui", "suffix:.pro", "suffix:.resx", "app.manifest", "tauri.conf", "suffix:.pyw"},
		Words: []string{"desktop app", "desktop application", "gui", "masaüstü", "windows forms", "wpf", "qt"},
	},
	{
		Name:  "CLI / Developer Tools",
		Deps:  []string{"spf13/cobra", "urfave/cli", "click", "typer", "clap", "commander", "yargs", "inquirer"},
		Files: []string{"cmd/", "cli/"},
		Words: []string{"cli", "command line", "command-line", "terminal", "developer tool", "plugin", "extension", "vscode extension", "library", "sdk", "komut satırı"},
	},
	{
		Name:  "Automation / Scraping",
		Deps:  []string{"selenium", "playwright", "puppeteer", "scrapy", "beautifulsoup4", "bs4", "requests-html", "celery", "airflow", "apache-airflow", "schedule", "pyautogui"},
		Langs: []string{"PowerShell", "Shell", "Batchfile", "AutoHotkey"},
		Words: []string{"automation", "scraper", "scraping", "crawler", "bot", "otomasyon", "workflow"},
	},
	{
		Name:  "Blockchain / Web3",
		Deps:  []string{"hardhat", "truffle", "web3", "ethers", "@solana/web3.js", "wagmi", "foundry"},
		Files: []string{"suffix:.sol", "hardhat.config", "foundry.toml"},
		Langs: []string{"Solidity", "Move", "Cairo"},
		Words: []string{"blockchain", "smart contract", "ethereum", "web3", "defi", "nft", "solana"},
	},
	{
		Name:  "Cybersecurity",
		Deps:  []string{"scapy", "pwntools", "impacket", "volatility3", "yara-python"},
		Words: []string{"security", "pentest", "penetration", "exploit", "vulnerability", "ctf", "malware", "reverse engineering", "forensics", "siber", "güvenlik", "osint", "fuzzing"},
	},
}

var Sectors = []Category{
	{Name: "Finance / Fintech", Words: []string{"bank", "banking", "payment", "payments", "fintech", "trading", "stock", "stocks", "forex", "invoice", "accounting", "finance", "financial", "loan", "credit card", "wallet", "portfolio", "banka", "ödeme", "borsa", "muhasebe", "fatura", "finans", "kredi"}},
	{Name: "Healthcare", Words: []string{"health", "healthcare", "medical", "patient", "hospital", "clinic", "dicom", "fhir", "hl7", "diagnosis", "ecg", "ekg", "mri", "sağlık", "hasta", "hastane", "tıbbi", "klinik"}},
	{Name: "E-commerce / Retail", Words: []string{"e-commerce", "ecommerce", "shop", "shopping", "cart", "checkout", "online store", "product catalog", "retail", "marketplace", "order management", "e-ticaret", "sepet", "mağaza", "sipariş", "ürün"}},
	{Name: "Education", Words: []string{"education", "learning", "course", "student", "students", "school", "lms", "quiz", "teacher", "university", "eğitim", "öğrenci", "okul", "ders", "öğretmen", "üniversite"}},
	{Name: "Automotive / Mobility", Words: []string{"automotive", "vehicle", "vehicles", "car", "cars", "can bus", "canbus", "autosar", "obd", "ecu", "adas", "autonomous driving", "traffic", "araç", "otomotiv", "trafik"}},
	{Name: "Defense / Aerospace", Words: []string{"defense", "defence", "military", "drone", "uav", "radar", "avionics", "satellite", "aerospace", "missile", "flight controller", "px4", "ardupilot", "savunma", "iha", "uydu", "havacılık", "askeri"}},
	{Name: "Telecom / Networking", Words: []string{"telecom", "telecommunication", "5g", "lte", "sip", "voip", "router", "networking", "network protocol", "sdn", "packet", "telekom", "haberleşme"}},
	{Name: "Energy", Words: []string{"energy", "solar", "battery", "bms", "power grid", "smart grid", "inverter", "photovoltaic", "ev charging", "enerji", "güneş", "batarya", "şarj"}},
	{Name: "Industrial / Manufacturing", Words: []string{"plc", "scada", "modbus", "industrial", "manufacturing", "factory", "opc ua", "opcua", "cnc", "robotics", "robot", "endüstri", "endüstriyel", "üretim", "fabrika"}},
	{Name: "Gaming / Entertainment", Words: []string{"game", "games", "gaming", "esports", "oyun", "entertainment", "eğlence"}},
	{Name: "Media / Social", Words: []string{"social", "social media", "chat", "messaging", "video", "streaming", "music", "podcast", "blog", "news", "media", "sosyal", "haber", "müzik"}},
	{Name: "Logistics / Supply Chain", Words: []string{"logistics", "shipping", "fleet", "warehouse", "inventory", "supply chain", "delivery", "tracking", "lojistik", "depo", "stok", "kargo", "teslimat"}},
	{Name: "Agriculture", Words: []string{"agriculture", "farm", "farming", "crop", "irrigation", "greenhouse", "tarım", "sera", "sulama"}},
	{Name: "Real Estate", Words: []string{"real estate", "property", "rental", "housing", "emlak", "kiralama"}},
	{Name: "Government / Legal", Words: []string{"government", "e-government", "legal", "law", "court", "public sector", "kamu", "hukuk", "belediye"}},
	{Name: "Human Resources", Words: []string{"human resources", "recruitment", "hiring", "payroll", "employee", "attendance", "insan kaynakları", "bordro", "personel"}},
	{Name: "Travel / Tourism", Words: []string{"travel", "tourism", "hotel", "booking", "flight", "reservation", "turizm", "otel", "rezervasyon", "seyahat"}},
	{Name: "Cybersecurity", Words: []string{"security", "cybersecurity", "pentest", "malware", "vulnerability", "ctf", "siber güvenlik"}},
	{Name: "Blockchain / Crypto", Words: []string{"blockchain", "crypto", "cryptocurrency", "bitcoin", "ethereum", "defi", "nft", "web3", "kripto"}},
}

// Manifests are dependency files in priority order. Only a few are fetched per repo.
var Manifests = []string{
	"package.json", "requirements.txt", "pyproject.toml", "go.mod", "cargo.toml",
	"pom.xml", "build.gradle", "build.gradle.kts", "pubspec.yaml", "platformio.ini",
	"composer.json", "gemfile", "pipfile", "cmakelists.txt", "environment.yml", "setup.py",
}

// Markup languages are shown in the report but excluded from the weighted share.
var Markup = map[string]bool{
	"HTML": true, "CSS": true, "SCSS": true, "Less": true, "Sass": true, "Stylus": true,
	"TeX": true, "Makefile": true, "CMake": true, "Dockerfile": true, "Batchfile": true,
	"Procfile": true, "Roff": true,
}
