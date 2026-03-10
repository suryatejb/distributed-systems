// Runner for CMUD.

package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/suryatejb/kvclient"
)

type fixedAddressRouter struct {
	address string
}

func (router fixedAddressRouter) NextAddr() string {
	return router.address
}

// Game Constants
var name = "" // character name
var validLocations = [...]string{
	"fence",
	"bridge",
	"1",
	"2",
	"3",
	"4",
	"5",
	"6",
	"7",
	"8",
	"9",
}

var hardware = [...]string{
	"cpu",
	"gpu",
	"asic",
	"quantum",
	"dark_matter",
}

var prices = map[string]int{
	"cpu":         1,
	"gpu":         5,
	"asic":        25,
	"quantum":     125,
	"dark_matter": 625,
}

var powers = map[string]int{
	"cpu":         1,
	"gpu":         10,
	"asic":        100,
	"quantum":     1000,
	"dark_matter": 10000,
}

var education = [...]string{
	"Undergraduate",
	"Masters",
	"PhD",
	"PostDoc",
	"Professor",
	"Retired",
}

var problemHealth = map[string]uint64{
	"Undergraduate": 100,
	"Masters":       1000,
	"PhD":           10000,
	"PostDoc":       500000,
	"Professor":     10000000,
	"Retired":       0,
}

var cmdsOrder = [...]string{
	"help",
	"stats",
	"map",
	"move `location`",
	"mine `num`",
	"shop",
	"buy `computer`",
	"inv",
	"solve",
}

var cmds = map[string]string{
	"help":            "Prints help message",
	"stats":           "Prints stats",
	"map":             "Display everyone on the map",
	"move `location`": "Move to location",
	"mine `num`":      "Try to mine bitcoins for `num` of times",
	"shop":            "Display the shop",
	"buy `computer`":  "Buy new hardware",
	"inv":             "Display your inventory",
	"solve":           "Solve next problem (enter battle!)",
}

var solveCmdsOrder = [...]string{
	"solve",
	"resign",
	"inv",
}

var solveCmds = map[string]string{
	"solve":  "Compute part of the problem",
	"resign": "Give up on the problem",
	"inv":    "Display your inventory",
}

const (
	delim             = "/"
	event             = "---"
	eduPrefix         = "edu/"
	locPrefix         = "loc/"
	balancePrefix     = "balance/"
	compPrefix        = "comp/"
	solvePrefix       = "solve/"
	storyTime         = 2
	miningSuccessRate = 8
	miningRange       = 10
	miningTime        = 5
	computingTime     = 5
	breakSuccess      = 2
	breakRate         = 4 // larger is less breaking
	solveSuccess      = 18
	solveRange        = 20
)

// Get / Put / List error checking wrappers

func Get(cli *kvclient.Client, key string) (string, bool) {
	value, ok, err := cli.Get(key)
	if err != nil {
		fmt.Println(err)
		Error("Get request failed.")
	}
	return value, ok
}

func Put(cli *kvclient.Client, key string, value string) {
	err := cli.Put(key, value)
	if err != nil {
		fmt.Println(err)
		Error("Put request failed.")
	}
}

func List(cli *kvclient.Client, prefix string) map[string]string {
	entries, err := cli.List(prefix)
	if err != nil {
		fmt.Println(err)
		Error("List request failed.")
	}
	return entries
}

// Print Statements
func EventMessage(message string) {
	fmt.Println("---", message, "---")
}

func Error(message string) {
	panic("Error -- " + message)
}

func PrintWelcomeMessage() {
	fmt.Print(
		`██████╗ ███╗   ███╗██╗   ██╗██████╗
██╔════╝████╗ ████║██║   ██║██╔══██╗
██║     ██╔████╔██║██║   ██║██║  ██║
██║     ██║╚██╔╝██║██║   ██║██║  ██║
╚██████╗██║ ╚═╝ ██║╚██████╔╝██████╔` + "\n\n")

	fmt.Print("Welcome to CMUD, a MUD game based around 'CMU' and 'D'istributed Systems. " +
		"For more information on MUD games, check out: https://en.wikipedia.org/wiki/MUD \n\n")
}

func StoryTimeSleep() {
	time.Sleep(time.Duration(storyTime) * time.Second)
}

func PrintIntroMessage() {
	fmt.Println("It's 2010, and you've just enrolled at Carnegie Mellon University as an undergraduate Computer Science major.")
	StoryTimeSleep()
	fmt.Println("Bright and eager, you decided to audit this course all of the upperclass students have been raving about...")
	StoryTimeSleep()
	fmt.Println("15-440!")
	StoryTimeSleep()
	fmt.Println("While sitting in the course, you caught wind of your classmates talking about this new coin that uses bits...")
	StoryTimeSleep()
	fmt.Println("You wonder, what could you do with these coins?")
	StoryTimeSleep()
	fmt.Println("None the wiser, you decide to use your at-home laptop to start mining...")
	StoryTimeSleep()
}

func PrintStartStory() {
	StoryTimeSleep()
	fmt.Print(".\n")
	StoryTimeSleep()
	fmt.Print(".\n")
	StoryTimeSleep()
	fmt.Print(".\n")
	StoryTimeSleep()
}

func PrintMastersStory() {
	fmt.Println("You did it!")
	StoryTimeSleep()
	fmt.Println("You used your machines...")
	StoryTimeSleep()
	fmt.Println("and brute-forced an NP-Hard problem.")
	StoryTimeSleep()
	fmt.Println("But you feel a little unsatisfied with your accomplishment...")
	StoryTimeSleep()
	fmt.Println("You want to solve bigger problems.")
	StoryTimeSleep()
	fmt.Println("What about 100 nodes? or 1000 nodes?")
	StoryTimeSleep()
	fmt.Println("Looking for more experience, you enroll in the 5th year BS/MS program here at Carnegie Mellon University...")
	StoryTimeSleep()
	fmt.Println("...and decided to take 15-640.")
	StoryTimeSleep()
}

func PrintPhDStory() {
	fmt.Println("You did it!")
	StoryTimeSleep()
	fmt.Println("You used your machines...")
	StoryTimeSleep()
	fmt.Println("and completed your midterm in time.")
	StoryTimeSleep()
	fmt.Println("but you still want more.")
	StoryTimeSleep()
	fmt.Println("You want to solve bigger problems.")
	StoryTimeSleep()
	fmt.Println("Deep learning is starting to become the hype nowadays.")
	StoryTimeSleep()
	fmt.Println("Hopping on the hype-train, you apply to the PhD program here at Carnegie Mellon University...")
	StoryTimeSleep()
	fmt.Println("...and decided to conduct deep learning research.")
	StoryTimeSleep()
}

func PrintPostDocStory() {
	fmt.Println("You did it!")
	StoryTimeSleep()
	fmt.Println("You used your machines...")
	StoryTimeSleep()
	fmt.Println("and finished training the newest state-of-the-art NLP model.")
	StoryTimeSleep()
	fmt.Println("Your new AI is its own intelligent life-form.")
	StoryTimeSleep()
	fmt.Println("Maybe this AI can help you solve bigger problems within theoretical computer science...")
	StoryTimeSleep()
	fmt.Println("Looking to create more impact, you continue a PostDoctoral program here at Carnegie Mellon University...")
	StoryTimeSleep()
	fmt.Println("...and decided to explore some age-old questions.")
	StoryTimeSleep()
}

func PrintProfessorStory() {
	fmt.Println("You did it!")
	StoryTimeSleep()
	fmt.Println("You used your machines...")
	StoryTimeSleep()
	fmt.Println("and solved P=NP!")
	StoryTimeSleep()
	fmt.Println("You collected your millions from Clay Mathematics Institute.")
	StoryTimeSleep()
	fmt.Println("Now you buy more machines...")
	StoryTimeSleep()
	fmt.Println("...to help you answer deeper questions.")
	StoryTimeSleep()
	fmt.Println("You apply to be a Professor here at Carnegie Mellon University...")
	StoryTimeSleep()
	fmt.Println("...and decided to ask more meaningful questions.")
	StoryTimeSleep()
}

func PrintRetiredStory() {
	fmt.Println("After years of running...")
	StoryTimeSleep()
	fmt.Println("all of your machines finally stopped.")
	StoryTimeSleep()
	fmt.Println("it's done computing.")
	StoryTimeSleep()
	fmt.Println("Hurriedly, you rush over to your tried-and-true terminal screen")
	StoryTimeSleep()
	fmt.Println("and see the final result of the program...")
	StoryTimeSleep()
	fmt.Println("...")
	StoryTimeSleep()
	fmt.Println("42")
	StoryTimeSleep()
	fmt.Println("...")
	StoryTimeSleep()
	fmt.Println("ha ha")
	StoryTimeSleep()
	fmt.Println("You laugh to yourself...")
	StoryTimeSleep()
	fmt.Println("...what a journey it has been.")
	StoryTimeSleep()
	fmt.Println("Thank you for playing.")
	StoryTimeSleep()
}

func PrintStoryLine(edu string) {
	PrintStartStory()
	switch edu {
	case "Masters":
		PrintMastersStory()
	case "PhD":
		PrintPhDStory()
	case "PostDoc":
		PrintPostDocStory()
	case "Professor":
		PrintProfessorStory()
	case "Retired":
		PrintRetiredStory()
	}
	if edu != "Retired" {
		fmt.Println("(new machines added to your shop)")
	}
}

func PrintHelpMessage() {
	EventMessage("List of Commands")
	for _, k := range cmdsOrder {
		fmt.Println(k, "-", cmds[k])
	}
}

func PrintSolveHelpMessage() {
	EventMessage("List of Battle Commands")
	for _, k := range solveCmdsOrder {
		fmt.Println(k, "-", solveCmds[k])
	}
}

func PrintCreateNewCharater() {
	fmt.Printf("Enter your new character name!\n")
}

func PrintWelcome(name string) {
	fmt.Printf("Welcome %s!\n", name)
}

func PrintWelcomeBack(name string) {
	fmt.Printf("Welcome back %s!\n", name)
}

func PrintStats(cli *kvclient.Client) {
	fmt.Println("Name:", name)
	edu := GetEducation(cli)
	fmt.Println("Education:", edu)
}

func PrintBalance(balance uint64) {
	fmt.Printf("Bitcoins: %d\n", balance)
}

func PrintUnknownLocation(loc string) {
	fmt.Println("(404) Location not found:", loc)
}

func PrettifyLocName(loc string) string {
	switch loc {
	case "fence":
		return "The Fence (fence)"
	case "bridge":
		return "Randy Pausch Bridge (bridge)"
	case "1":
		return "Gates 1st Floor (1)"
	case "2":
		return "Gates 2nd Floor (2)"
	case "3":
		return "Gates 3rd Floor (3)"
	case "4":
		return "Gates 4th Floor (4)"
	case "5":
		return "Gates 5th Floor (5)"
	case "6":
		return "Gates 6th Floor (6)"
	case "7":
		return "Gates 7th Floor (7)"
	case "8":
		return "Gates 8th Floor (8)"
	case "9":
		return "Gates 9th Floor (9)"
	}
	Error("Unknown location name: " + loc)
	return "unreachable"
}

func PrettifyHardwareName(comp string) string {
	switch comp {
	case "cpu":
		return "Xeon CPUs"
	case "gpu":
		return "Nvidia GPUs"
	case "asic":
		return "Bitcoin ASICs"
	case "quantum":
		return "Quantum Processors"
	case "dark_matter":
		return "Dark Matter Processors"
	}
	Error("Unknown hardware name: " + comp)
	return "unreachable"
}

func PrintValidLocations() {
	fmt.Println("Here is the directory of valid locations:")
	for _, l := range validLocations {
		fmt.Println(PrettifyLocName(l))
	}
	fmt.Println("To move, use the command `move <loc>`, e.g. `move 1`")
}

func PrintLocMessage(loc string) {
	switch loc {
	case "fence", "bridge":
		fmt.Printf("You are now at the %s.\n", loc)
	case "1":
		fmt.Printf("You are now on the %sst floor.\n", loc)
	case "2":
		fmt.Printf("You are now on the %snd floor.\n", loc)
	case "3":
		fmt.Printf("You are now on the %srd floor.\n", loc)
	default:
		fmt.Printf("You are now on the %sth floor.\n", loc)
	}
}

func PrintShop(cli *kvclient.Client) {
	levelIdx := GetEducationIndex(cli)
	EventMessage("Shop")
	for _, h := range hardware[:levelIdx] {
		p := prices[h]
		fmt.Println(PrettifyHardwareName(h) + " (" + h + "): " + strconv.Itoa(p) + " bitcoin(s)")
	}
}

func PrintInventory(cli *kvclient.Client, computers map[string]int) {
	EventMessage("Inventory")
	levelIdx := GetEducationIndex(cli)
	for _, h := range hardware[:levelIdx] {
		numComputers := computers[h]
		fmt.Println(PrettifyHardwareName(h)+":", numComputers)
	}
}

func PrintUndergradateProblem() {
	fmt.Println("You've encountered your first problem...")
	StoryTimeSleep()
	fmt.Println("the traveling saleman problem")
	StoryTimeSleep()
	fmt.Println("with 50 nodes.")
	StoryTimeSleep()
	fmt.Println("Oh well, you do have some computers.")
	StoryTimeSleep()
	fmt.Println("Let's just try and brute force it!")
	StoryTimeSleep()
}

func PrintMastersProblem() {
	fmt.Println("You've encountered your second problem...")
	StoryTimeSleep()
	fmt.Println("the 15-640 take-home midterm.")
	StoryTimeSleep()
	fmt.Println("Oh! That's not too bad, at least it's take-home, you thought as you start reading through the exam.")
	StoryTimeSleep()
	fmt.Println("Problem 1: Decrypt this SHA-1 hash.")
	StoryTimeSleep()
	fmt.Println("Hmm... do I have enough computing resources to solve this?")
	StoryTimeSleep()
}

func PrintPhDProblem() {
	fmt.Println("You've encountered your third problem...")
	StoryTimeSleep()
	fmt.Println("training the newest NLP model...")
	StoryTimeSleep()
	fmt.Println("GPT-4")
	StoryTimeSleep()
	fmt.Println("Luckily, training these deep learning jobs are highly parallelizable to train...")
	StoryTimeSleep()
	fmt.Println("Time to start counting these epochs!")
	StoryTimeSleep()
}

func PrintPostDocProblem() {
	fmt.Println("You've encountered your fourth problem...")
	StoryTimeSleep()
	fmt.Println("With the advancement of quantum computing...")
	StoryTimeSleep()
	fmt.Println("you decide to take on the age old question")
	StoryTimeSleep()
	fmt.Println("Does P = NP?")
	StoryTimeSleep()
	fmt.Println("Well, obviously if N == ...")
	StoryTimeSleep()
	fmt.Println("But maybe there's a better answer out there...")
	StoryTimeSleep()
}

func PrintProfessorProblem() {
	fmt.Println("You've encountered your fifth problem...")
	StoryTimeSleep()
	fmt.Println("You want to understand...")
	StoryTimeSleep()
	fmt.Println("How do you reach enlightenment?")
	StoryTimeSleep()
	fmt.Println("How to obtain true happiness?")
	StoryTimeSleep()
	fmt.Println("What is the true meaning of life?")
	StoryTimeSleep()
	fmt.Println("Maybe with this Dark Matter processor, you'll be able to find something...")
	StoryTimeSleep()
}

func PrintProblem(problem string) {
	switch problem {
	case "Undergraduate":
		PrintUndergradateProblem()
	case "Masters":
		PrintMastersProblem()
	case "PhD":
		PrintPhDProblem()
	case "PostDoc":
		PrintPostDocProblem()
	case "Professor":
		PrintProfessorProblem()
	case "Retired":
		fmt.Println("Let's leave these new problems for younger generations to solve.")
	}
}

func InitializeCharacter(cli *kvclient.Client) {
	_, ok := Get(cli, name)
	if ok {
		PrintWelcomeBack(name)
		PrintStats(cli)
		fmt.Println("")
	} else {
		Put(cli, name, "Undergraduate")

		// initialize location
		locKey := locPrefix + name
		Put(cli, locKey, "5")

		// initialize balance
		balanceKey := balancePrefix + name
		Put(cli, balanceKey, "0")

		// initialize inventory
		compKey := compPrefix + name + delim + "cpu"
		Put(cli, compKey, "1")

		// Welcome new character!
		PrintWelcome(name)

		PrintIntroMessage()
	}
}

func InitializeProblems(cli *kvclient.Client) {
	for _, p := range education {
		_, ok := Get(cli, p)
		if !ok {
			Put(cli, p, strconv.FormatUint(problemHealth[p], 10))
		}
	}
}

func GetEducation(cli *kvclient.Client) string {
	edu, ok := Get(cli, name)
	if ok {
		return edu
	}
	Error("Client does not have a level initialized")
	return "unreachable"
}

func GetEducationIndex(cli *kvclient.Client) int {
	edu := GetEducation(cli)
	for i, l := range education {
		if l == edu {
			if i == len(education)-1 {
				return i
			} else {
				return i + 1
			}
		}
	}
	Error("Client education: " + edu)
	return 0 // unreachable
}

// Movement
func validateLocation(loc string) bool {
	for _, l := range validLocations {
		if loc == l {
			return true
		}
	}
	return false
}

func move(cli *kvclient.Client, loc string) {
	if validateLocation(loc) {
		key := locPrefix + name
		value := loc
		err := cli.Put(key, value)
		if err != nil {
			fmt.Println("Error:", err)
		}
		PrintLocMessage(loc)
	} else {
		PrintUnknownLocation(loc)
		PrintValidLocations()
	}
}

func showMap(cli *kvclient.Client) {
	locs := make(map[string][]string)
	locations := List(cli, locPrefix)
	for key, value := range locations {
		n := strings.Split(key, delim)[1]
		if _, ok := locs[value]; ok {
			locs[value] = append(locs[value], n)
		} else {
			locs[value] = []string{n}
		}
	}
	for _, l := range validLocations {
		if people, ok := locs[l]; ok {
			EventMessage(PrettifyLocName(l))
			fmt.Println(strings.Join(people, ","))
		}
	}
}

func getBalance(cli *kvclient.Client) uint64 {
	var balance uint64
	var err error
	key := balancePrefix + name
	value, ok := Get(cli, key)
	if ok {
		balance, err = strconv.ParseUint(value, 10, 64)
		if err != nil {
			Error("Balance was not a uint64")
		}
	} else {
		Error("default balance should at least be 0 for: " + name)
	}
	return balance
}

func validateComputer(computer string) bool {
	for _, c := range hardware {
		if computer == c {
			return true
		}
	}
	return false
}

func getComputers(cli *kvclient.Client) map[string]int {
	computers := make(map[string]int)
	compKey := compPrefix + name
	inventory := List(cli, compKey)
	for key, value := range inventory {
		comp := strings.Split(key, delim)[2]
		num, err := strconv.Atoi(value)
		if err != nil {
			Error("Expected computing power to be an int")
		}
		if num > 0 {
			computers[comp] = num
		}
	}
	return computers
}

func getComputingPower(cli *kvclient.Client) int {
	computers := getComputers(cli)
	totalPower := 0
	for c, n := range computers {
		totalPower += powers[c] * n
	}
	return totalPower
}

func getMinedCoins(cli *kvclient.Client) uint64 {
	totalPower := getComputingPower(cli)
	return uint64(rand.Intn(totalPower)) + 1
}

func mine(cli *kvclient.Client, num int) {
	for i := 1; i <= num; i++ {
		EventMessage(fmt.Sprintf("Mining bitcoin [%d/%d]...", i, num))
		sleepTime := time.Duration(rand.Intn(miningTime))
		time.Sleep(sleepTime * time.Second)
		balance := getBalance(cli)
		mined := rand.Intn(miningRange)
		if mined <= miningSuccessRate {
			minedCoins := getMinedCoins(cli)
			newBalance := strconv.FormatUint(balance+minedCoins, 10)
			key := balancePrefix + name
			Put(cli, key, newBalance)
			EventMessage(fmt.Sprintf("Successfully mined %d coin(s)!", minedCoins))
			PrintBalance(balance + minedCoins)
		} else {
			EventMessage("Didn't mine a coin.")
			PrintBalance(balance)
		}
	}
}

func buy(cli *kvclient.Client, comp string) {
	balance := getBalance(cli)
	computers := getComputers(cli)
	compKey := compPrefix + name + delim + comp
	compPrice := uint64(prices[comp])

	if balance >= compPrice {
		newNumComputers := 0
		for balance >= compPrice {
			balance -= compPrice
			newNumComputers++
		}
		EventMessage("You have purchased: " + strconv.Itoa(newNumComputers) + " " + PrettifyHardwareName(comp))
		if numComputers, ok := computers[comp]; ok {
			newNumComputers += numComputers
		}
		computers[comp] = newNumComputers
		// update balance
		Put(cli, balancePrefix+name, strconv.FormatUint(balance, 10))
		// update number of computers
		Put(cli, compKey, strconv.Itoa(newNumComputers))
	} else {
		EventMessage("You could not afford: " + comp)
	}
}

func ComputeProblem(cli *kvclient.Client, problem string) bool {
	EventMessage("Computing problem...")
	sleepTime := time.Duration(rand.Intn(computingTime))
	time.Sleep(sleepTime * time.Second)

	roll := rand.Intn(solveRange)
	if roll <= breakSuccess {
		// broke a hardware component
		var total int
		computers := getComputers(cli)
		breakHardware := rand.Intn(len(computers))
		index := 0
		for k, v := range computers {
			if breakHardware == index {
				fmt.Println("Oh no! We broke some of our " + PrettifyHardwareName(k))
				computers[k] = v / breakRate
				total += v / breakRate
			} else {
				total += v
			}
			index++
		}

		// update computing power
		for k, v := range computers {
			compKey := compPrefix + name + delim + k
			Put(cli, compKey, strconv.Itoa(v))
		}

		// lost the battle
		if total == 0 {
			StoryTimeSleep()
			fmt.Println("You run out of computing resources...")
			StoryTimeSleep()
			fmt.Println("With no other options, you login to a Hunt Library computer...")
			StoryTimeSleep()
			fmt.Println("Time to start mining again...")
			StoryTimeSleep()

			// give back starting CPU
			compKey := compPrefix + name + delim + "cpu"
			Put(cli, compKey, "1")
			return true
		}
		return false
	} else if roll <= solveSuccess {
		// solved part of the problem
		fmt.Println("Success! We solved part of the problem!")
		computingPower := uint64(getComputingPower(cli))
		var remainingHealth uint64
		var err error
		value, ok := Get(cli, problem)
		if !ok {
			Put(cli, problem, strconv.FormatUint(problemHealth[problem], 10))
			remainingHealth = problemHealth[problem]
		} else {
			remainingHealth, err = strconv.ParseUint(value, 10, 64)
			if err != nil {
				Error("Remaining health was not an uint64")
			}
		}
		if remainingHealth > computingPower {
			fmt.Printf("The problem has %d remaining steps.\n", remainingHealth-computingPower)
			Put(cli, problem, strconv.FormatUint(remainingHealth-computingPower, 10))
			return false
		} else {
			fmt.Println("The problem has been solved!")
			Put(cli, problem, strconv.FormatUint(problemHealth[problem], 10))
			var nextEducation string = education[len(education)-1]
			for i, edu := range education {
				if edu == problem && i < len(education)-2 {
					nextEducation = education[i+1]
					break
				}
			}
			Put(cli, name, nextEducation)
			PrintStoryLine(nextEducation)
			return true
		}
	} else {
		// got stuck debugging
		fmt.Println("Stuck debugging again... no progress made...")
		return false
	}
}

func SolveProblem(cli *kvclient.Client, reader *bufio.Reader) {
	problem := GetEducation(cli)
	PrintProblem(problem)
	if problem == education[len(education)-1] {
		return
	}
	for {
		fmt.Printf("(solving) > ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(4)
		}
		line = strings.TrimSpace(line)
		words := strings.Split(line, " ")
		if len(words) == 0 {
			PrintSolveHelpMessage()
			continue
		}
		switch words[0] {
		case "inv":
			balance := getBalance(cli)
			PrintBalance(balance)
			computers := getComputers(cli)
			PrintInventory(cli, computers)
		case "solve":
			if ComputeProblem(cli, problem) {
				return
			}
		case "resign":
			fmt.Println("Giving up on the problem... let's come back another day.")
			return
		default:
			PrintSolveHelpMessage()
		}
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run cmud.go <request actor address>")
		os.Exit(5)
	}
	address := os.Args[1]

	router := &fixedAddressRouter{address}
	cli := kvclient.NewClient(router)

	PrintWelcomeMessage()
	PrintCreateNewCharater()
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(4)
		}
		line = strings.TrimSpace(line)
		words := strings.Split(line, " ")

		if name == "" {
			if len(words) == 0 || len(words[0]) == 0 {
				PrintCreateNewCharater()
			} else {
				name = words[0]
				InitializeCharacter(cli)
				InitializeProblems(cli)
				break
			}
		}
	}

	for {
		fmt.Printf("(mining) > ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(4)
		}
		line = strings.TrimSpace(line)
		words := strings.Split(line, " ")

		if name == "" {
			if len(words) == 0 {
				PrintCreateNewCharater()
			} else {
				name = words[0]
				InitializeCharacter(cli)
				InitializeProblems(cli)
			}
			continue
		}

		if len(words) == 0 {
			PrintHelpMessage()
			continue
		}
		switch words[0] {
		case "help":
			PrintHelpMessage()
		case "stats":
			PrintStats(cli)
		case "move":
			if len(words) == 2 {
				move(cli, words[1])
			} else {
				PrintUnknownLocation("`empty`")
				PrintValidLocations()
			}
		case "map":
			showMap(cli)
		case "mine":
			if len(words) == 2 {
				num, err := strconv.Atoi(words[1])
				if err != nil {
					Error("Expected number of mining activities to be an int")
				}
				mine(cli, num)
			} else {
				mine(cli, 1)
			}
		case "inv":
			balance := getBalance(cli)
			PrintBalance(balance)
			computers := getComputers(cli)
			PrintInventory(cli, computers)
		case "shop":
			PrintShop(cli)
		case "buy":
			if len(words) == 2 {
				if validateComputer(words[1]) {
					buy(cli, words[1])
				} else {
					fmt.Println("Unknown computer: " + words[1])
				}
			} else {
				fmt.Println("Unknown computer: " + `empty`)
			}
		case "solve":
			SolveProblem(cli, reader)
		default:
			PrintHelpMessage()
		}
	}
}
