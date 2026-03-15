package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fuse/internal/api"
	"fuse/internal/client"
	"fuse/internal/domain"
	"fuse/internal/recipes"
	"fuse/internal/server"
	"fuse/internal/tui"
)

const (
	defaultDirectSSHHost  = "user44@184.34.82.180"
	defaultGuaranteedGPUs = 16
)

type cliAPI interface {
	Status(ctx context.Context) (domain.ClusterStatus, error)
	Nodes(ctx context.Context) ([]domain.Node, []domain.Device, error)
	Fabric(ctx context.Context) ([]domain.FabricLink, error)
	Teams(ctx context.Context) ([]domain.Team, error)
	Jobs(ctx context.Context) ([]domain.Job, error)
	Events(ctx context.Context, limit int) ([]domain.Event, error)
	Storage(ctx context.Context, target string) (domain.StorageStatus, error)
	Topology(ctx context.Context, req domain.TopologyRequest) (domain.TopologyProbe, error)
	Shard(ctx context.Context, req domain.ShardRequest) (domain.ShardPlan, error)
	Submit(ctx context.Context, spec domain.JobSpec) (domain.Job, error)
	Logs(ctx context.Context, jobID, stream string, tailLines int) (domain.JobLog, error)
	Why(ctx context.Context, jobID string) (domain.Why, error)
	Cancel(ctx context.Context, jobID string) error
	Checkpoint(ctx context.Context, jobID string) error
	Checkpoints(ctx context.Context, jobID string) ([]domain.Checkpoint, error)
	Simulate(ctx context.Context, req domain.SimulationRequest) (domain.SimulationResult, error)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if wantsHelp(os.Args[1:]) {
		usage()
		return
	}
	if shouldLaunchDefaultTUI(os.Args[1:]) {
		runTUI(ctx, os.Args[1:])
		return
	}
	switch os.Args[1] {
	case "server":
		runServer(ctx, os.Args[2:])
	case "status":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, _ []string, jsonOut bool) error {
			status, err := cli.Status(ctx)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(status)
			}
			fmt.Printf("nodes=%d devices=%d allocated=%d idle=%d running=%d pending=%d failed=%d\n",
				status.Nodes, status.Devices, status.Allocated, status.Idle, status.RunningJobs, status.PendingJobs, status.FailedJobs,
			)
			return nil
		})
	case "nodes":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, _ []string, jsonOut bool) error {
			nodes, devices, err := cli.Nodes(ctx)
			if err != nil {
				return err
			}
			if nodes == nil {
				nodes = []domain.Node{}
			}
			if devices == nil {
				devices = []domain.Device{}
			}
			payload := map[string]any{"nodes": nodes, "devices": devices}
			if jsonOut {
				return printJSON(payload)
			}
			for _, node := range nodes {
				count := 0
				switchName := node.SwitchName
				if switchName == "" {
					switchName = "-"
				}
				for _, device := range devices {
					if device.NodeID == node.ID {
						count++
					}
				}
				totalGPUs := count
				if node.TotalGPUs > 0 {
					totalGPUs = node.TotalGPUs
				}
				fmt.Printf("%s\t%s\tgpus=%d\talloc=%d\tfree=%d\tstate=%s\thealth=%s\treal=%t\n",
					node.Name, switchName, totalGPUs, node.AllocatedGPUs, node.FreeGPUs, defaultString(node.ObservedState, "-"), node.Health, node.Real,
				)
			}
			return nil
		})
	case "fabric":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, _ []string, jsonOut bool) error {
			links, err := cli.Fabric(ctx)
			if err != nil {
				return err
			}
			if links == nil {
				links = []domain.FabricLink{}
			}
			if jsonOut {
				return printJSON(links)
			}
			for _, link := range links {
				fmt.Printf("%s -> %s\t%s\t%dGbps\n", link.SourceNodeID, link.TargetNodeID, link.Tier, link.BandwidthGbps)
			}
			return nil
		})
	case "teams":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, _ []string, jsonOut bool) error {
			teams, err := cli.Teams(ctx)
			if err != nil {
				return err
			}
			if teams == nil {
				teams = []domain.Team{}
			}
			if jsonOut {
				return printJSON(teams)
			}
			for _, team := range teams {
				fmt.Printf("%s\tquota=%d\tburst=%t\tgpu_hours=%.1f\n", team.Name, team.QuotaGPUs, team.BurstEnabled, team.GPUHours)
			}
			return nil
		})
	case "jobs":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, _ []string, jsonOut bool) error {
			jobs, err := cli.Jobs(ctx)
			if err != nil {
				return err
			}
			if jobs == nil {
				jobs = []domain.Job{}
			}
			if jsonOut {
				return printJSON(jobs)
			}
			for _, job := range jobs {
				slurmID := job.SlurmJobID
				if slurmID == "" {
					slurmID = "-"
				}
				nodes := "-"
				if len(job.NodeList) > 0 {
					nodes = strings.Join(job.NodeList, ",")
				}
				fmt.Printf("%s\t%s\tteam=%s\tgpus=%d\tslurm=%s\tnodes=%s\traw=%s\n", job.ID, job.State, job.Team, job.GPUs, slurmID, nodes, job.RawState)
			}
			return nil
		})
	case "events":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, _ []string, jsonOut bool) error {
			events, err := cli.Events(ctx, 20)
			if err != nil {
				return err
			}
			if events == nil {
				events = []domain.Event{}
			}
			if jsonOut {
				return printJSON(events)
			}
			for _, event := range events {
				fmt.Printf("%s\t%s\t%s\n", event.CreatedAt.Format(time.RFC3339), event.ReasonCode, event.Summary)
			}
			return nil
		})
	case "storage":
		withClient(ctx, os.Args[2:], showStorage)
	case "topo":
		withClient(ctx, os.Args[2:], showTopology)
	case "shard":
		withClient(ctx, os.Args[2:], showShard)
	case "submit":
		withClient(ctx, os.Args[2:], submitSpec)
	case "run":
		withClient(ctx, os.Args[2:], submitRun)
	case "train":
		withClient(ctx, os.Args[2:], submitTrain)
	case "finetune":
		withClient(ctx, os.Args[2:], submitFinetune)
	case "logs":
		withClient(ctx, os.Args[2:], showLogs)
	case "why":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
			fs := flag.NewFlagSet("why", flag.ExitOnError)
			jobID := fs.String("job", "", "fuse job id")
			_ = fs.Parse(args)
			if *jobID == "" && fs.NArg() > 0 {
				*jobID = fs.Arg(0)
			}
			if *jobID == "" {
				return fmt.Errorf("job id is required")
			}
			why, err := cli.Why(ctx, *jobID)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(why)
			}
			fmt.Printf("reason=%s\nsummary=%s\ndetail=%s\nstate=%s\nraw=%s\n", why.ReasonCode, why.Summary, why.Detail, why.CurrentState, why.RawState)
			if why.SlurmJobID != "" {
				fmt.Printf("slurm_job_id=%s\n", why.SlurmJobID)
			}
			if len(why.NodeList) > 0 {
				fmt.Printf("nodes=%s\n", strings.Join(why.NodeList, ","))
			}
			if why.ExitCode != nil {
				fmt.Printf("exit_code=%d\n", *why.ExitCode)
			}
			for _, suggestion := range why.Suggestions {
				fmt.Printf("suggestion: %s\n", suggestion)
			}
			return nil
		})
	case "cancel":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, args []string, _ bool) error {
			fs := flag.NewFlagSet("cancel", flag.ExitOnError)
			jobID := fs.String("job", "", "fuse job id")
			_ = fs.Parse(args)
			if *jobID == "" && fs.NArg() > 0 {
				*jobID = fs.Arg(0)
			}
			if *jobID == "" {
				return fmt.Errorf("job id is required")
			}
			return cli.Cancel(ctx, *jobID)
		})
	case "checkpoint":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, args []string, _ bool) error {
			fs := flag.NewFlagSet("checkpoint", flag.ExitOnError)
			jobID := fs.String("job", "", "fuse job id")
			_ = fs.Parse(args)
			if *jobID == "" && fs.NArg() > 0 {
				*jobID = fs.Arg(0)
			}
			if *jobID == "" {
				return fmt.Errorf("job id is required")
			}
			return cli.Checkpoint(ctx, *jobID)
		})
	case "checkpoints":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
			fs := flag.NewFlagSet("checkpoints", flag.ExitOnError)
			jobID := fs.String("job", "", "fuse job id")
			_ = fs.Parse(args)
			if *jobID == "" && fs.NArg() > 0 {
				*jobID = fs.Arg(0)
			}
			if *jobID == "" {
				return fmt.Errorf("job id is required")
			}
			checkpoints, err := cli.Checkpoints(ctx, *jobID)
			if err != nil {
				return err
			}
			if checkpoints == nil {
				checkpoints = []domain.Checkpoint{}
			}
			if jsonOut {
				return printJSON(checkpoints)
			}
			if len(checkpoints) == 0 {
				fmt.Println("no checkpoints")
				return nil
			}
			for _, cp := range checkpoints {
				fmt.Printf("%s\t%s\tverified=%t\t%s\n", cp.StepLabel, cp.Path, cp.Verified, cp.CreatedAt.Format(time.RFC3339))
			}
			return nil
		})
	case "simulate":
		withClient(ctx, os.Args[2:], func(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
			fs := flag.NewFlagSet("simulate", flag.ExitOnError)
			killNode := fs.String("kill-node", "", "node id to remove")
			addNodes := fs.Int("add-nodes", 0, "number of fake nodes to add")
			switchName := fs.String("switch", "", "switch name for added nodes")
			_ = fs.Parse(args)
			var req domain.SimulationRequest
			switch {
			case *killNode != "":
				req.Action = domain.SimulationKillNode
				req.NodeID = *killNode
			case *addNodes > 0:
				req.Action = domain.SimulationAddNode
				req.AddNodes = *addNodes
				req.SwitchName = *switchName
			default:
				return fmt.Errorf("one simulation action is required")
			}
			result, err := cli.Simulate(ctx, req)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(result)
			}
			fmt.Println(result.Summary)
			return nil
		})
	case "bench":
		runBench(ctx, os.Args[2:])
	case "tui":
		runTUI(ctx, os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func shouldLaunchDefaultTUI(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if wantsHelp(args) {
		return false
	}
	return strings.HasPrefix(args[0], "-")
}

func wantsHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "help", "-h", "--help":
		return true
	}
	return len(args) > 1 && (args[1] == "-h" || args[1] == "--help")
}

func runTUI(ctx context.Context, args []string) {
	if wantsHelp(append([]string{"tui"}, args...)) {
		usage()
		return
	}
	sourceLabel := tuiSourceLabel(args)
	withClient(ctx, args, func(ctx context.Context, cli cliAPI, _ []string, _ bool) error {
		return tui.Run(ctx, os.Stdout, cli, tui.Options{
			RefreshInterval: 2 * time.Second,
			SourceLabel:     sourceLabel,
		})
	})
}

func runServer(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:9090", "listen address")
	dbPath := fs.String("db", filepath.Join(".fuse", "state.db"), "sqlite database path")
	faker := fs.Bool("faker", true, "enable faker discovery")
	nvml := fs.Bool("nvml", false, "enable local nvidia-smi discovery")
	sshHost := fs.String("ssh-host", "", "remote SSH host for Slurm commands, e.g. user01@us-west-a2-login-001")
	guaranteedGPUs := fs.Int("guaranteed-gpus", defaultGuaranteedGPUs, "default guaranteed GPU quota for the primary team")
	artifacts := fs.String("artifacts-dir", filepath.Join(".fuse", "artifacts"), "artifacts directory")
	_ = fs.Parse(args)
	svc, err := server.New(ctx, server.Config{
		DBPath:         *dbPath,
		Faker:          *faker,
		WithNVML:       *nvml,
		SSHHost:        *sshHost,
		GuaranteedGPUs: *guaranteedGPUs,
		ArtifactsDir:   *artifacts,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer svc.Close()
	svc.Start(ctx)
	httpServer := &http.Server{
		Addr:    *addr,
		Handler: api.NewRouter(svc),
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	fmt.Printf("Fuse server listening on http://%s\n", *addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func withClient(ctx context.Context, args []string, fn func(context.Context, cliAPI, []string, bool) error) {
	addr := ""
	dbPath := envOrDefaultString("FUSE_DB", filepath.Join(".fuse", "state.db"))
	artifactsDir := envOrDefaultString("FUSE_ARTIFACTS_DIR", filepath.Join(".fuse", "artifacts"))
	sshHost := envOrDefaultString("FUSE_SSH_HOST", defaultDirectSSHHost)
	faker := false
	nvml := false
	guaranteedGPUs := envOrDefaultInt("FUSE_GUARANTEED_GPUS", defaultGuaranteedGPUs)
	jsonOut := false
	sshHostExplicit := false
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		switch args[i] {
		case "--addr":
			if i+1 >= len(args) {
				log.Fatal("--addr requires a value")
			}
			addr = args[i+1]
			i++
		case "--db":
			if i+1 >= len(args) {
				log.Fatal("--db requires a value")
			}
			dbPath = args[i+1]
			i++
		case "--artifacts-dir":
			if i+1 >= len(args) {
				log.Fatal("--artifacts-dir requires a value")
			}
			artifactsDir = args[i+1]
			i++
		case "--ssh-host":
			if i+1 >= len(args) {
				log.Fatal("--ssh-host requires a value")
			}
			sshHost = args[i+1]
			sshHostExplicit = true
			i++
		case "--guaranteed-gpus":
			if i+1 >= len(args) {
				log.Fatal("--guaranteed-gpus requires a value")
			}
			value, err := strconv.Atoi(args[i+1])
			if err != nil {
				log.Fatalf("--guaranteed-gpus must be an integer: %v", err)
			}
			guaranteedGPUs = value
			i++
		case "--faker":
			faker = true
		case "--nvml":
			nvml = true
		case "--json":
			jsonOut = true
		default:
			rest = append(rest, args[i])
		}
	}
	sshHost = resolveDirectSSHHost(sshHost, sshHostExplicit, addr, faker, nvml)
	var (
		cli     cliAPI
		closeFn func() error
	)
	if addr != "" {
		cli = client.New(addr)
	} else {
		direct, err := newDirectCLI(ctx, server.Config{
			DBPath:         dbPath,
			Faker:          faker,
			WithNVML:       nvml,
			SSHHost:        sshHost,
			GuaranteedGPUs: guaranteedGPUs,
			ArtifactsDir:   artifactsDir,
		})
		if err != nil {
			log.Fatal(err)
		}
		cli = direct
		closeFn = direct.Close
	}
	if closeFn != nil {
		defer func() {
			if err := closeFn(); err != nil {
				log.Printf("close failed: %v", err)
			}
		}()
	}
	if err := fn(ctx, cli, rest, jsonOut); err != nil {
		log.Fatal(err)
	}
}

func submitRun(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	name := fs.String("name", fmt.Sprintf("run-%d", time.Now().Unix()), "job name")
	team := fs.String("team", "default", "team name")
	gpus := fs.Int("gpus", 1, "gpu count")
	cpus := fs.Int("cpus", 4, "cpus per task")
	mem := fs.Int64("mem-mb", 16*1024, "memory in MB")
	walltime := fs.String("time", "00:30:00", "walltime")
	topology := fs.String("topology", string(domain.TopologyAny), "topology hint")
	workdir := fs.String("workdir", "", "working directory")
	image := fs.String("image", "", "Slurm pyxis container image or squashfs path")
	containerWorkdir := fs.String("container-workdir", "", "working directory inside the container")
	mountHome := fs.Bool("mount-home", false, "mount the user's home directory inside the container")
	containerMounts := make([]string, 0)
	envVars := make(map[string]string)
	fs.Func("mount", "container mount SRC:DST[:FLAGS]; repeat to add more mounts", func(value string) error {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("mount cannot be empty")
		}
		containerMounts = append(containerMounts, value)
		return nil
	})
	fs.Func("env", "environment override KEY=VALUE; repeat to add more variables", func(value string) error {
		key, val, err := parseAssignment(value)
		if err != nil {
			return err
		}
		envVars[key] = val
		return nil
	})
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		return fmt.Errorf("command after -- is required")
	}
	if *workdir == "" {
		*workdir = ""
	}
	spec := domain.JobSpec{
		Name:               *name,
		Team:               *team,
		Type:               domain.JobTypeRun,
		CommandOrRecipe:    shellJoin(fs.Args()),
		Workdir:            *workdir,
		ContainerImage:     *image,
		ContainerMounts:    containerMounts,
		ContainerWorkdir:   *containerWorkdir,
		ContainerMountHome: *mountHome,
		Env:                envVars,
		GPUs:               *gpus,
		CPUs:               *cpus,
		MemoryMB:           *mem,
		Walltime:           *walltime,
		CheckpointMode:     domain.CheckpointNone,
		TopologyHint:       domain.TopologyHint(*topology),
	}
	job, err := cli.Submit(ctx, spec)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(job)
	}
	if job.SlurmJobID != "" {
		fmt.Printf("submitted job %s (%s) slurm=%s\n", job.ID, job.State, job.SlurmJobID)
		return nil
	}
	fmt.Printf("submitted job %s (%s)\n", job.ID, job.State)
	return nil
}

func submitTrain(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
	fs := flag.NewFlagSet("train", flag.ExitOnError)
	example := fs.String("example", "makemore", "training example: makemore, nanochat, or axolotl-probe")
	name := fs.String("name", "", "job name")
	team := fs.String("team", "default", "team name")
	gpus := fs.Int("gpus", 0, "gpu count; defaults per example")
	cpus := fs.Int("cpus", 0, "cpus per task; defaults per example")
	mem := fs.Int64("mem-mb", 0, "memory in MB; defaults per example")
	walltime := fs.String("time", "", "walltime; defaults per example")
	image := fs.String("image", "", "container image or sqsh path; defaults per example")
	sharedRoot := fs.String("shared-root", recipes.DefaultSharedRoot, "shared root mounted in the container")
	workloadRoot := fs.String("workload-root", "", "remote workload script root; defaults to <shared-root>/fuse-workloads")
	artifacts := fs.String("artifacts-dir", "", "artifacts directory")
	steps := fs.Int("steps", 0, "override training steps for the example")
	hold := fs.Int("hold", 0, "keep the job alive for N seconds after the training probe succeeds")
	mountHome := fs.Bool("mount-home", false, "mount the user's home directory inside the container")
	envVars := make(map[string]string)
	fs.Func("env", "environment override KEY=VALUE; repeat to add more variables", func(value string) error {
		key, val, err := parseAssignment(value)
		if err != nil {
			return err
		}
		envVars[key] = val
		return nil
	})
	_ = fs.Parse(args)
	if *name == "" {
		*name = fmt.Sprintf("%s-train-%d", strings.ToLower(strings.TrimSpace(*example)), time.Now().Unix())
	}
	if strings.TrimSpace(*workloadRoot) == "" {
		*workloadRoot = path.Join(*sharedRoot, "fuse-workloads")
	}
	artifactsDir := ""
	if *artifacts != "" {
		artifactsDir = path.Join(*artifacts, *name)
	}
	spec, err := recipes.BuildTrainSpec(recipes.TrainInput{
		Name:         *name,
		Team:         *team,
		Example:      *example,
		GPUs:         *gpus,
		CPUs:         *cpus,
		MemoryMB:     *mem,
		Walltime:     *walltime,
		Image:        *image,
		SharedRoot:   *sharedRoot,
		WorkloadRoot: *workloadRoot,
		Steps:        *steps,
		ArtifactsDir: artifactsDir,
		HoldSeconds:  *hold,
		MountHome:    *mountHome,
		Env:          envVars,
	})
	if err != nil {
		return err
	}
	job, err := cli.Submit(ctx, spec)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(job)
	}
	if job.SlurmJobID != "" {
		fmt.Printf("submitted train job %s (%s) slurm=%s\n", job.ID, job.State, job.SlurmJobID)
		return nil
	}
	fmt.Printf("submitted train job %s (%s)\n", job.ID, job.State)
	return nil
}

func submitFinetune(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
	fs := flag.NewFlagSet("finetune", flag.ExitOnError)
	name := fs.String("name", fmt.Sprintf("finetune-%d", time.Now().Unix()), "job name")
	team := fs.String("team", "default", "team name")
	model := fs.String("model", "meta-llama/Llama-2-7b-hf", "model name")
	data := fs.String("data", "./alpaca.json", "dataset path")
	workdir := fs.String("workdir", "", "working directory")
	artifacts := fs.String("artifacts-dir", "", "artifacts directory")
	_ = fs.Parse(args)
	if *workdir == "" {
		*workdir = ""
	}
	artifactsDir := ""
	if *artifacts != "" {
		artifactsDir = filepath.Join(*artifacts, *name)
	}
	spec := recipes.BuildFinetuneSpec(recipes.FinetuneInput{
		Name:         *name,
		Team:         *team,
		Model:        *model,
		Dataset:      *data,
		Workdir:      *workdir,
		GPUs:         1,
		Walltime:     "01:00:00",
		ArtifactsDir: artifactsDir,
	})
	job, err := cli.Submit(ctx, spec)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(job)
	}
	if job.SlurmJobID != "" {
		fmt.Printf("submitted finetune job %s (%s) slurm=%s\n", job.ID, job.State, job.SlurmJobID)
		return nil
	}
	fmt.Printf("submitted finetune job %s (%s)\n", job.ID, job.State)
	return nil
}

func submitSpec(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	file := fs.String("file", "", "path to a JSON JobSpec file or - for stdin")
	_ = fs.Parse(args)
	if *file == "" && fs.NArg() > 0 {
		*file = fs.Arg(0)
	}
	if *file == "" {
		return fmt.Errorf("spec file is required")
	}
	spec, err := loadJobSpec(*file)
	if err != nil {
		return err
	}
	job, err := cli.Submit(ctx, spec)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(job)
	}
	if job.SlurmJobID != "" {
		fmt.Printf("submitted job %s (%s) slurm=%s\n", job.ID, job.State, job.SlurmJobID)
		return nil
	}
	fmt.Printf("submitted job %s (%s)\n", job.ID, job.State)
	return nil
}

func showStorage(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
	fs := flag.NewFlagSet("storage", flag.ExitOnError)
	target := fs.String("path", "", "filesystem path to inspect")
	_ = fs.Parse(args)
	status, err := cli.Storage(ctx, *target)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(status)
	}
	for _, fs := range status.Filesystems {
		fmt.Printf("%s\t%s\tsize=%s\tused=%s\tavail=%s\tuse=%d%%\n",
			fs.Target,
			fs.Source,
			humanBytes(fs.SizeBytes),
			humanBytes(fs.UsedBytes),
			humanBytes(fs.AvailableBytes),
			fs.UsePercent,
		)
	}
	return nil
}

func showTopology(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
	fs := flag.NewFlagSet("topo", flag.ExitOnError)
	jobID := fs.String("job", "", "fuse job id")
	slurmJobID := fs.String("slurm-job", "", "Slurm job id")
	node := fs.String("node", "", "specific node to probe")
	gpus := fs.Int("gpus", 8, "GPU count for an ephemeral topology probe")
	cpus := fs.Int("cpus", 16, "CPUs per task for an ephemeral topology probe")
	mem := fs.Int64("mem-mb", 100*1024, "memory in MB for an ephemeral topology probe")
	walltime := fs.String("time", "00:05:00", "walltime for an ephemeral topology probe")
	immediate := fs.Int("immediate", 60, "seconds to wait for an ephemeral probe allocation")
	_ = fs.Parse(args)
	probe, err := cli.Topology(ctx, domain.TopologyRequest{
		JobID:            *jobID,
		SlurmJobID:       *slurmJobID,
		Node:             *node,
		GPUs:             *gpus,
		CPUs:             *cpus,
		MemoryMB:         *mem,
		Walltime:         *walltime,
		ImmediateSeconds: *immediate,
	})
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(probe)
	}
	fmt.Printf("mode=%s\n", probe.Mode)
	if probe.JobID != "" {
		fmt.Printf("job_id=%s\n", probe.JobID)
	}
	if probe.SlurmJobID != "" {
		fmt.Printf("slurm_job_id=%s\n", probe.SlurmJobID)
	}
	if probe.RequestedGPUs > 0 {
		fmt.Printf("requested_gpus=%d\n", probe.RequestedGPUs)
	}
	for idx, node := range probe.Nodes {
		if idx > 0 {
			fmt.Println()
		}
		fmt.Printf("[%s]\n%s\n", node.Node, node.Matrix)
	}
	return nil
}

func showShard(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
	fs := flag.NewFlagSet("shard", flag.ExitOnError)
	model := fs.String("model", "", "model profile name, e.g. llama-70b")
	gpus := fs.Int("gpus", 0, "total GPU count")
	nodes := fs.Int("nodes", 0, "node count override")
	method := fs.String("method", "full", "memory model: full, lora, or inference")
	_ = fs.Parse(args)
	if *model == "" {
		return fmt.Errorf("model is required")
	}
	if *gpus <= 0 {
		return fmt.Errorf("gpus must be greater than zero")
	}
	plan, err := cli.Shard(ctx, domain.ShardRequest{
		Model:  *model,
		GPUs:   *gpus,
		Nodes:  *nodes,
		Method: *method,
	})
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(plan)
	}
	fmt.Printf("model=%s\nmethod=%s\ngpus=%d\nnodes=%d\ngpus_per_node=%d\ntp=%d\npp=%d\ndp=%d\nper_gpu_weight_gb=%.1f\nestimated_per_gpu_memory_gb=%.1f\ndevice_memory_gb=%.1f\ntopology=%s\nsummary=%s\ndetail=%s\n",
		plan.Model,
		plan.Method,
		plan.GPUs,
		plan.Nodes,
		plan.GPUsPerNode,
		plan.TensorParallel,
		plan.PipelineParallel,
		plan.DataParallel,
		plan.PerGPUWeightGB,
		plan.EstimatedPerGPUMemoryGB,
		plan.DeviceMemoryGB,
		plan.TopologyHint,
		plan.Summary,
		plan.Detail,
	)
	for _, suggestion := range plan.Suggestions {
		fmt.Printf("suggestion: %s\n", suggestion)
	}
	return nil
}

func showLogs(ctx context.Context, cli cliAPI, args []string, jsonOut bool) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	jobID := fs.String("job", "", "fuse job id")
	stream := fs.String("stream", "stdout", "log stream: stdout or stderr")
	tail := fs.Int("tail", 200, "tail the last N lines, use 0 for the full file")
	_ = fs.Parse(args)
	if *jobID == "" && fs.NArg() > 0 {
		*jobID = fs.Arg(0)
	}
	if *jobID == "" {
		return fmt.Errorf("job id is required")
	}
	logs, err := cli.Logs(ctx, *jobID, *stream, *tail)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(logs)
	}
	fmt.Print(logs.Content)
	if logs.Content != "" && !strings.HasSuffix(logs.Content, "\n") {
		fmt.Println()
	}
	return nil
}

func runBench(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	dbPath := fs.String("db", filepath.Join(".fuse", "state.db"), "sqlite database path")
	_ = fs.Parse(args)
	svc, err := server.New(ctx, server.Config{
		DBPath:         *dbPath,
		Faker:          false,
		WithNVML:       true,
		GuaranteedGPUs: 16,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer svc.Close()
	benchmarks, err := svc.BenchmarkLocalGPU(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if err := printJSON(benchmarks); err != nil {
		log.Fatal(err)
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func loadJobSpec(path string) (domain.JobSpec, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return domain.JobSpec{}, err
	}
	var spec domain.JobSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return domain.JobSpec{}, fmt.Errorf("decode job spec: %w", err)
	}
	return spec, nil
}

func parseAssignment(value string) (string, string, error) {
	key, raw, ok := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return "", "", fmt.Errorf("expected KEY=VALUE, got %q", value)
	}
	return key, raw, nil
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func humanBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%dB", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	size := float64(value)
	unit := ""
	for _, candidate := range units {
		size /= 1024
		unit = candidate
		if size < 1024 {
			break
		}
	}
	return fmt.Sprintf("%.1f%s", size, unit)
}

func envOrDefaultString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Fatalf("%s must be an integer: %v", key, err)
	}
	return parsed
}

func resolveDirectSSHHost(current string, explicit bool, addr string, faker, nvml bool) string {
	if explicit {
		return current
	}
	if addr != "" || faker || nvml {
		return ""
	}
	return current
}

func tuiSourceLabel(args []string) string {
	addr := ""
	sshHost := envOrDefaultString("FUSE_SSH_HOST", defaultDirectSSHHost)
	sshHostExplicit := false
	faker := false
	nvml := false
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		switch args[i] {
		case "--addr":
			if i+1 < len(args) {
				addr = args[i+1]
				i++
			}
		case "--ssh-host":
			if i+1 < len(args) {
				sshHost = args[i+1]
				sshHostExplicit = true
				i++
			}
		case "--faker":
			faker = true
		case "--nvml":
			nvml = true
		}
	}
	sshHost = resolveDirectSSHHost(sshHost, sshHostExplicit, addr, faker, nvml)
	switch {
	case addr != "":
		return "remote " + strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	case faker:
		return "faker"
	case nvml:
		return "nvml"
	case sshHost != "":
		return "live " + sshHost
	default:
		return "live"
	}
}

func usage() {
	fmt.Println("Fuse")
	fmt.Println()
	fmt.Println("Usage")
	fmt.Println("  fuse [--faker|--addr URL|--ssh-host HOST]    Launch the TUI")
	fmt.Println("  fuse tui [--faker|--addr URL|--ssh-host HOST]")
	fmt.Println("  fuse <command> [flags]")
	fmt.Println()
	fmt.Println("First steps")
	fmt.Println("  make build     Build ./fuse and refresh the legacy ./.bin/fuse-live wrapper")
	fmt.Println("  ./fuse --help  Show this help")
	fmt.Println("  ./fuse --faker Launch the local fake-cluster TUI")
	fmt.Println("  make install   Install fuse into ~/.local/bin (or PREFIX)")
	fmt.Println()
	fmt.Println("Binary paths")
	fmt.Println("  ./fuse             Canonical repo-local binary")
	fmt.Println("  fuse               Installed binary after make install")
	fmt.Println("  ./.bin/fuse-live   Generated compatibility wrapper for older scripts")
	fmt.Println()
	fmt.Println("Top commands")
	fmt.Println("  status        Cluster summary")
	fmt.Println("  nodes         Nodes and devices")
	fmt.Println("  jobs          Jobs table")
	fmt.Println("  events        Recent scheduling events")
	fmt.Println("  topo          Topology probe")
	fmt.Println("  shard         Sharding recommendation")
	fmt.Println("  run           Submit an interactive run job")
	fmt.Println("  train         Submit a training job")
	fmt.Println("  finetune      Submit a fine-tune job")
	fmt.Println("  logs          Tail job logs")
	fmt.Println("  why           Explain scheduler state")
	fmt.Println("  checkpoint    Trigger a checkpoint")
	fmt.Println("  checkpoints   List checkpoints for a job")
	fmt.Println("  simulate      Run a simulation action")
	fmt.Println("  bench         Run local benchmark helpers")
	fmt.Println()
	fmt.Println("TUI discoverability")
	fmt.Println("  q             Quit")
	fmt.Println("  tab / shift+tab")
	fmt.Println("                Move focus between panes")
	fmt.Println("  :             Open the command bar")
	fmt.Println("  r             Refresh immediately")
	fmt.Println("  ?             Toggle the key help footer")
	fmt.Println()
	fmt.Println("Command bar examples")
	fmt.Println("  :nodes")
	fmt.Println("  :jobs")
	fmt.Println("  :events")
	fmt.Println("  :refresh")
	fmt.Println("  :help")
	fmt.Println("  :quit")
}
