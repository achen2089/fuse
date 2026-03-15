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
		printHelp(os.Args[1:])
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
	for idx, arg := range args {
		if arg == "--" {
			break
		}
		if idx == 0 && arg == "help" {
			return true
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func runTUI(ctx context.Context, args []string) {
	if wantsHelp(append([]string{"tui"}, args...)) {
		printHelp(append([]string{"tui"}, args...))
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
	nodes := fs.Int("nodes", 0, "node count override for multi-node examples")
	gpusPerNode := fs.Int("gpus-per-node", 0, "GPUs per node override for multi-node examples")
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
		Nodes:        *nodes,
		GPUsPerNode:  *gpusPerNode,
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

type helpFlag struct {
	Name        string
	Description string
}

type helpTopic struct {
	Name     string
	Summary  string
	Usage    []string
	Notes    []string
	Flags    []helpFlag
	Examples []string
	SeeAlso  []string
}

type helpPair struct {
	Label       string
	Description string
}

func printHelp(args []string) {
	if topic := helpTopicFromArgs(args); topic != "" {
		if printCommandHelp(topic) {
			return
		}
		fmt.Printf("Unknown help topic %q.\n\n", topic)
	}
	usage()
}

func helpTopicFromArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if args[0] == "help" {
		for _, arg := range args[1:] {
			if arg == "--" || arg == "-h" || arg == "--help" {
				break
			}
			return arg
		}
		return ""
	}
	if strings.HasPrefix(args[0], "-") {
		return ""
	}
	for _, arg := range args[1:] {
		if arg == "--" {
			break
		}
		if arg == "-h" || arg == "--help" {
			return args[0]
		}
	}
	return ""
}

func printCommandHelp(name string) bool {
	topic, ok := lookupHelpTopic(name)
	if !ok {
		return false
	}
	fmt.Println("Fuse " + topic.Name)
	fmt.Println()
	fmt.Println(topic.Summary)
	fmt.Println()
	printHelpSection("Usage", topic.Usage...)
	if len(topic.Notes) > 0 {
		printHelpSection("Notes", topic.Notes...)
	}
	if len(topic.Flags) > 0 {
		printHelpFlags("Flags", topic.Flags)
	}
	if usesSharedConnectionFlags(topic.Name) {
		printHelpFlags("Shared connection flags", sharedConnectionFlags())
	}
	if len(topic.Examples) > 0 {
		printHelpSection("Examples", topic.Examples...)
	}
	if len(topic.SeeAlso) > 0 {
		printHelpSection("See also", topic.SeeAlso...)
	}
	return true
}

func printHelpSection(title string, lines ...string) {
	if len(lines) == 0 {
		return
	}
	fmt.Println(title)
	for _, line := range lines {
		fmt.Println("  " + line)
	}
	fmt.Println()
}

func printHelpFlags(title string, flags []helpFlag) {
	if len(flags) == 0 {
		return
	}
	fmt.Println(title)
	for _, flag := range flags {
		fmt.Printf("  %-26s %s\n", flag.Name, flag.Description)
	}
	fmt.Println()
}

func printHelpPairs(title string, pairs ...helpPair) {
	if len(pairs) == 0 {
		return
	}
	fmt.Println(title)
	for _, pair := range pairs {
		fmt.Printf("  %-48s %s\n", pair.Label, pair.Description)
	}
	fmt.Println()
}

func sharedConnectionFlags() []helpFlag {
	return []helpFlag{
		{Name: "--faker", Description: "use the built-in fake cluster; safest place to learn the CLI and TUI"},
		{Name: "--addr URL", Description: "talk to a running Fuse HTTP server instead of direct mode"},
		{Name: "--ssh-host HOST", Description: "override the SSH login host used by direct live mode"},
		{Name: "--nvml", Description: "probe the local machine with NVIDIA discovery instead of SSH"},
		{Name: "--json", Description: "emit JSON instead of human-readable text"},
		{Name: "--db PATH", Description: "override the sqlite state path used in direct or server-backed workflows"},
		{Name: "--artifacts-dir PATH", Description: "override the artifacts root for generated files and recipes"},
		{Name: "--guaranteed-gpus N", Description: "override the default guaranteed GPU quota in direct mode"},
	}
}

func usesSharedConnectionFlags(name string) bool {
	switch name {
	case "server", "bench":
		return false
	default:
		return true
	}
}

func lookupHelpTopic(name string) (helpTopic, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "tui":
		return helpTopic{
			Name:    "tui",
			Summary: "Launch the Bubble Tea dashboard. Bare `fuse` is the same entrypoint as `fuse tui`.",
			Usage: []string{
				"fuse",
				"fuse --faker",
				"fuse tui [connection flags]",
			},
			Notes: []string{
				"Leading flags with no command launch the TUI, so `fuse --faker` opens the dashboard while `fuse status --faker` runs the status command.",
				"The dashboard refreshes automatically and supports an inline command bar for pane switching and quick actions.",
				"Use the fake cluster first if you are learning the navigation or verifying the binary works.",
			},
			Examples: []string{
				"./fuse --faker",
				"./fuse --addr http://127.0.0.1:9090",
				"./fuse tui --ssh-host user44@cluster-login",
				"Keys: q quit, r refresh, tab shift focus, / or : open the command bar, ? toggle help",
				"Command bar: :nodes, :jobs, :events, :refresh, :help, :quit",
			},
			SeeAlso: []string{
				"fuse --help",
				"fuse help status",
				"fuse help jobs",
			},
		}, true
	case "status":
		return helpTopic{
			Name:    "status",
			Summary: "Print a one-line cluster summary: nodes, devices, allocated GPUs, idle GPUs, and running or pending job counts.",
			Usage: []string{
				"fuse status [connection flags]",
			},
			Notes: []string{
				"Use this first to check that Fuse can reach the target cluster or fake environment.",
				"`--json` is the easiest mode for scripts and health checks.",
			},
			Examples: []string{
				"./fuse status --faker",
				"./fuse status --addr http://127.0.0.1:9090",
				"./fuse status --json --ssh-host user44@cluster-login",
			},
			SeeAlso: []string{
				"fuse help nodes",
				"fuse help jobs",
				"fuse help events",
			},
		}, true
	case "nodes":
		return helpTopic{
			Name:    "nodes",
			Summary: "List nodes and their device inventory, allocation state, switch placement, and health.",
			Usage: []string{
				"fuse nodes [connection flags]",
			},
			Notes: []string{
				"Human-readable output shows the node name, switch, GPU counts, alloc or free counts, observed state, health, and whether the node is real or synthetic.",
				"`--json` returns both `nodes` and `devices` arrays for automation.",
			},
			Examples: []string{
				"./fuse nodes --faker",
				"./fuse nodes --json --addr http://127.0.0.1:9090",
			},
			SeeAlso: []string{
				"fuse help fabric",
				"fuse help topo",
			},
		}, true
	case "fabric":
		return helpTopic{
			Name:    "fabric",
			Summary: "Show fabric links between nodes, including link tier and bandwidth.",
			Usage: []string{
				"fuse fabric [connection flags]",
			},
			Notes: []string{
				"Use this when you need a fast text view of node-to-node connectivity outside the TUI.",
			},
			Examples: []string{
				"./fuse fabric --faker",
				"./fuse fabric --json --addr http://127.0.0.1:9090",
			},
			SeeAlso: []string{
				"fuse help nodes",
				"fuse help topo",
			},
		}, true
	case "teams":
		return helpTopic{
			Name:    "teams",
			Summary: "List team quotas, burst status, and accumulated GPU hours.",
			Usage: []string{
				"fuse teams [connection flags]",
			},
			Examples: []string{
				"./fuse teams --faker",
				"./fuse teams --json --addr http://127.0.0.1:9090",
			},
			SeeAlso: []string{
				"fuse help status",
				"fuse help jobs",
			},
		}, true
	case "jobs":
		return helpTopic{
			Name:    "jobs",
			Summary: "List current jobs with Fuse id, scheduler state, team, GPU count, Slurm id, and node allocation.",
			Usage: []string{
				"fuse jobs [connection flags]",
			},
			Notes: []string{
				"This is the fastest way to see which jobs are running, pending, or failed without opening the TUI.",
				"Use `fuse help why`, `fuse help logs`, and `fuse help cancel` once you have the job id.",
			},
			Examples: []string{
				"./fuse jobs --faker",
				"./fuse jobs --json --addr http://127.0.0.1:9090",
			},
			SeeAlso: []string{
				"fuse help logs",
				"fuse help why",
				"fuse help cancel",
			},
		}, true
	case "events":
		return helpTopic{
			Name:    "events",
			Summary: "Show the latest scheduler events with timestamp, reason code, and summary.",
			Usage: []string{
				"fuse events [connection flags]",
			},
			Notes: []string{
				"The CLI currently fetches the 20 most recent events.",
			},
			Examples: []string{
				"./fuse events --faker",
				"./fuse events --json --addr http://127.0.0.1:9090",
			},
			SeeAlso: []string{
				"fuse help status",
				"fuse help jobs",
				"fuse help why",
			},
		}, true
	case "storage":
		return helpTopic{
			Name:    "storage",
			Summary: "Inspect filesystem capacity and usage for the target path or the default storage view.",
			Usage: []string{
				"fuse storage [connection flags]",
				"fuse storage [connection flags] --path /mnt/sharefs",
			},
			Flags: []helpFlag{
				{Name: "--path PATH", Description: "filesystem path to inspect"},
			},
			Examples: []string{
				"./fuse storage --faker",
				"./fuse storage --path /mnt/sharefs --addr http://127.0.0.1:9090",
			},
			SeeAlso: []string{
				"fuse help status",
			},
		}, true
	case "topo", "topology":
		return helpTopic{
			Name:    "topo",
			Summary: "Probe placement topology for an existing job or request a short-lived allocation to inspect fabric layout.",
			Usage: []string{
				"fuse topo [connection flags] --job <fuse-job-id>",
				"fuse topo [connection flags] --slurm-job <slurm-id>",
				"fuse topo [connection flags] --gpus 8 --cpus 16 --mem-mb 102400 --time 00:05:00",
			},
			Notes: []string{
				"Use an existing job id when you want to understand where something already landed.",
				"Use the ephemeral probe mode when you want to test allocation quality before launching a larger workload.",
			},
			Flags: []helpFlag{
				{Name: "--job ID", Description: "Fuse job id to inspect"},
				{Name: "--slurm-job ID", Description: "Slurm job id to inspect"},
				{Name: "--node NAME", Description: "probe or focus a specific node"},
				{Name: "--gpus N", Description: "GPU count for an ephemeral probe allocation"},
				{Name: "--cpus N", Description: "CPU count for an ephemeral probe allocation"},
				{Name: "--mem-mb MB", Description: "memory request for an ephemeral probe allocation"},
				{Name: "--time HH:MM:SS", Description: "walltime for an ephemeral probe allocation"},
				{Name: "--immediate SECONDS", Description: "seconds to wait for the ephemeral probe allocation"},
			},
			Examples: []string{
				"./fuse topo --faker --job run-123",
				"./fuse topo --addr http://127.0.0.1:9090 --slurm-job 481516",
				"./fuse topo --ssh-host user44@cluster-login --gpus 8 --cpus 16 --mem-mb 102400 --time 00:05:00",
			},
			SeeAlso: []string{
				"fuse help shard",
				"fuse help run",
			},
		}, true
	case "shard":
		return helpTopic{
			Name:    "shard",
			Summary: "Estimate tensor, pipeline, and data-parallel sharding for a model and GPU budget.",
			Usage: []string{
				"fuse shard [connection flags] --model <profile> --gpus <count>",
			},
			Flags: []helpFlag{
				{Name: "--model NAME", Description: "model profile name, for example llama-70b"},
				{Name: "--gpus N", Description: "total GPU count"},
				{Name: "--nodes N", Description: "optional node-count override"},
				{Name: "--method MODE", Description: "memory model: full, lora, or inference"},
			},
			Examples: []string{
				"./fuse shard --model llama-70b --gpus 16 --faker",
				"./fuse shard --model llama-70b --gpus 16 --nodes 2 --method full --json --addr http://127.0.0.1:9090",
			},
			SeeAlso: []string{
				"fuse help topo",
				"fuse help train",
				"fuse help finetune",
			},
		}, true
	case "submit":
		return helpTopic{
			Name:    "submit",
			Summary: "Submit a raw JSON JobSpec from a file or stdin. Use this when the built-in command helpers are too restrictive.",
			Usage: []string{
				"fuse submit [connection flags] --file job.json",
				"fuse submit [connection flags] --file - < job.json",
			},
			Flags: []helpFlag{
				{Name: "--file PATH", Description: "path to a JSON JobSpec file, or `-` for stdin"},
			},
			Examples: []string{
				"./fuse submit --faker --file examples/job.json",
				"./fuse submit --addr http://127.0.0.1:9090 --file - < job.json",
			},
			SeeAlso: []string{
				"fuse help run",
				"fuse help train",
				"fuse help finetune",
			},
		}, true
	case "run":
		return helpTopic{
			Name:    "run",
			Summary: "Submit an ad-hoc command as a job. Everything after `--` becomes the remote command line.",
			Usage: []string{
				"fuse run [connection flags] [run flags] -- <command> [args...]",
			},
			Notes: []string{
				"Use this for one-off commands, quick smoke tests, interactive shells, and simple containerized experiments.",
				"Repeat `--env KEY=VALUE` and `--mount SRC:DST[:FLAGS]` as many times as you need.",
				"If you omit the command after `--`, Fuse will fail before submission.",
			},
			Flags: []helpFlag{
				{Name: "--name NAME", Description: "job name"},
				{Name: "--team NAME", Description: "team name"},
				{Name: "--gpus N", Description: "GPU count"},
				{Name: "--cpus N", Description: "CPUs per task"},
				{Name: "--mem-mb MB", Description: "memory in MB"},
				{Name: "--time HH:MM:SS", Description: "walltime"},
				{Name: "--topology HINT", Description: "topology hint, for example any or same_switch"},
				{Name: "--workdir DIR", Description: "working directory outside the container"},
				{Name: "--image PATH", Description: "Pyxis container image or squashfs path"},
				{Name: "--container-workdir DIR", Description: "working directory inside the container"},
				{Name: "--mount-home", Description: "mount the user's home directory inside the container"},
				{Name: "--mount SRC:DST[:FLAGS]", Description: "container mount; repeat to add more mounts"},
				{Name: "--env KEY=VALUE", Description: "environment override; repeat to add more variables"},
			},
			Examples: []string{
				"./fuse run --faker --name smoke --gpus 1 -- bash -lc 'nvidia-smi'",
				"./fuse run --addr http://127.0.0.1:9090 --name shell --image /mnt/sharefs/user44/pytorch.sqsh --mount /mnt/sharefs:/mnt/sharefs -- bash -lc 'python -V'",
				"./fuse run --ssh-host user44@cluster-login --name debug --gpus 2 --topology same_switch --env NCCL_DEBUG=INFO -- python train.py",
			},
			SeeAlso: []string{
				"fuse help train",
				"fuse help logs",
				"fuse help why",
			},
		}, true
	case "train":
		return helpTopic{
			Name:    "train",
			Summary: "Submit a built-in training recipe. This is the easiest way to run a known-good example workload.",
			Usage: []string{
				"fuse train [connection flags] [train flags]",
			},
			Notes: []string{
				"`--example` currently supports `makemore`, `nanochat`, and `axolotl-probe`.",
				"Recipe defaults fill in image, resources, and command details so you do not need to author a full JobSpec.",
				"`--hold` is useful when you want the probe to stay alive briefly for inspection after it succeeds.",
			},
			Flags: []helpFlag{
				{Name: "--example NAME", Description: "training example: makemore, nanochat, or axolotl-probe"},
				{Name: "--name NAME", Description: "job name; autogenerated if omitted"},
				{Name: "--team NAME", Description: "team name"},
				{Name: "--gpus N", Description: "GPU count override"},
				{Name: "--cpus N", Description: "CPU count override"},
				{Name: "--mem-mb MB", Description: "memory override in MB"},
				{Name: "--time HH:MM:SS", Description: "walltime override"},
				{Name: "--image PATH", Description: "container image override"},
				{Name: "--shared-root DIR", Description: "shared root mounted inside the container"},
				{Name: "--workload-root DIR", Description: "remote workload script root"},
				{Name: "--artifacts-dir DIR", Description: "artifact root; Fuse appends the job name"},
				{Name: "--steps N", Description: "override training steps for the recipe"},
				{Name: "--hold SECONDS", Description: "sleep for N seconds after the recipe succeeds"},
				{Name: "--mount-home", Description: "mount the user's home directory inside the container"},
				{Name: "--env KEY=VALUE", Description: "environment override; repeat to add more variables"},
			},
			Examples: []string{
				"./fuse train --faker --example makemore --steps 200",
				"./fuse train --addr http://127.0.0.1:9090 --example nanochat --gpus 4 --name nanochat-smoke",
				"./fuse train --ssh-host user44@cluster-login --example axolotl-probe --hold 120",
			},
			SeeAlso: []string{
				"fuse help run",
				"fuse help finetune",
				"fuse help logs",
			},
		}, true
	case "finetune":
		return helpTopic{
			Name:    "finetune",
			Summary: "Submit the built-in fine-tuning recipe around a model and dataset path.",
			Usage: []string{
				"fuse finetune [connection flags] [flags]",
			},
			Flags: []helpFlag{
				{Name: "--name NAME", Description: "job name"},
				{Name: "--team NAME", Description: "team name"},
				{Name: "--model MODEL", Description: "model identifier"},
				{Name: "--data PATH", Description: "dataset path"},
				{Name: "--workdir DIR", Description: "working directory"},
				{Name: "--artifacts-dir DIR", Description: "artifact root; Fuse appends the job name"},
			},
			Examples: []string{
				"./fuse finetune --faker --model meta-llama/Llama-2-7b-hf --data ./alpaca.json",
				"./fuse finetune --addr http://127.0.0.1:9090 --name llama-ft --model meta-llama/Llama-2-7b-hf --data /mnt/sharefs/datasets/alpaca.json",
			},
			SeeAlso: []string{
				"fuse help train",
				"fuse help logs",
			},
		}, true
	case "logs":
		return helpTopic{
			Name:    "logs",
			Summary: "Fetch stdout or stderr for a job.",
			Usage: []string{
				"fuse logs [connection flags] --job <fuse-job-id>",
				"fuse logs [connection flags] <fuse-job-id>",
			},
			Flags: []helpFlag{
				{Name: "--job ID", Description: "Fuse job id"},
				{Name: "--stream NAME", Description: "log stream: stdout or stderr"},
				{Name: "--tail N", Description: "tail the last N lines; 0 means the full file"},
			},
			Examples: []string{
				"./fuse logs --faker --job run-123",
				"./fuse logs --addr http://127.0.0.1:9090 run-123 --stream stderr --tail 500",
			},
			SeeAlso: []string{
				"fuse help jobs",
				"fuse help why",
				"fuse help cancel",
			},
		}, true
	case "why":
		return helpTopic{
			Name:    "why",
			Summary: "Explain a job's current scheduler state, raw state, suggestions, and placement details when available.",
			Usage: []string{
				"fuse why [connection flags] --job <fuse-job-id>",
				"fuse why [connection flags] <fuse-job-id>",
			},
			Flags: []helpFlag{
				{Name: "--job ID", Description: "Fuse job id"},
			},
			Examples: []string{
				"./fuse why --faker --job run-123",
				"./fuse why --addr http://127.0.0.1:9090 run-123 --json",
			},
			SeeAlso: []string{
				"fuse help jobs",
				"fuse help logs",
				"fuse help topo",
			},
		}, true
	case "cancel":
		return helpTopic{
			Name:    "cancel",
			Summary: "Cancel a running or pending job.",
			Usage: []string{
				"fuse cancel [connection flags] --job <fuse-job-id>",
				"fuse cancel [connection flags] <fuse-job-id>",
			},
			Flags: []helpFlag{
				{Name: "--job ID", Description: "Fuse job id"},
			},
			Examples: []string{
				"./fuse cancel --faker --job run-123",
				"./fuse cancel --addr http://127.0.0.1:9090 run-123",
			},
			SeeAlso: []string{
				"fuse help jobs",
				"fuse help why",
			},
		}, true
	case "checkpoint":
		return helpTopic{
			Name:    "checkpoint",
			Summary: "Trigger a checkpoint for a job.",
			Usage: []string{
				"fuse checkpoint [connection flags] --job <fuse-job-id>",
				"fuse checkpoint [connection flags] <fuse-job-id>",
			},
			Flags: []helpFlag{
				{Name: "--job ID", Description: "Fuse job id"},
			},
			Examples: []string{
				"./fuse checkpoint --faker --job run-123",
				"./fuse checkpoint --addr http://127.0.0.1:9090 run-123",
			},
			SeeAlso: []string{
				"fuse help checkpoints",
			},
		}, true
	case "checkpoints":
		return helpTopic{
			Name:    "checkpoints",
			Summary: "List checkpoints that Fuse knows about for a job.",
			Usage: []string{
				"fuse checkpoints [connection flags] --job <fuse-job-id>",
				"fuse checkpoints [connection flags] <fuse-job-id>",
			},
			Flags: []helpFlag{
				{Name: "--job ID", Description: "Fuse job id"},
			},
			Examples: []string{
				"./fuse checkpoints --faker --job run-123",
				"./fuse checkpoints --addr http://127.0.0.1:9090 run-123 --json",
			},
			SeeAlso: []string{
				"fuse help checkpoint",
			},
		}, true
	case "simulate":
		return helpTopic{
			Name:    "simulate",
			Summary: "Apply a simulation action such as killing a node or adding synthetic nodes.",
			Usage: []string{
				"fuse simulate [connection flags] --kill-node <node-id>",
				"fuse simulate [connection flags] --add-nodes <count> [--switch <name>]",
			},
			Notes: []string{
				"Exactly one simulation action is required per invocation.",
				"This is mainly useful for demos, fake clusters, and scheduler testing.",
			},
			Flags: []helpFlag{
				{Name: "--kill-node ID", Description: "node id to remove"},
				{Name: "--add-nodes N", Description: "number of fake nodes to add"},
				{Name: "--switch NAME", Description: "switch name for newly added nodes"},
			},
			Examples: []string{
				"./fuse simulate --faker --kill-node node-1",
				"./fuse simulate --faker --add-nodes 2 --switch leaf-a",
			},
			SeeAlso: []string{
				"fuse help status",
				"fuse help nodes",
			},
		}, true
	case "server":
		return helpTopic{
			Name:    "server",
			Summary: "Run the local Fuse HTTP server. Client commands can then target it with `--addr`.",
			Usage: []string{
				"fuse server [flags]",
			},
			Flags: []helpFlag{
				{Name: "--addr HOST:PORT", Description: "listen address"},
				{Name: "--db PATH", Description: "sqlite database path"},
				{Name: "--faker", Description: "enable faker discovery"},
				{Name: "--nvml", Description: "enable local NVIDIA discovery"},
				{Name: "--ssh-host HOST", Description: "remote SSH host for Slurm commands"},
				{Name: "--guaranteed-gpus N", Description: "default guaranteed GPU quota for the primary team"},
				{Name: "--artifacts-dir DIR", Description: "artifacts directory"},
			},
			Examples: []string{
				"./fuse server --addr 127.0.0.1:9090 --faker",
				"./fuse server --addr 0.0.0.0:9090 --db .fuse/state.db --ssh-host user44@cluster-login",
				"./fuse status --addr http://127.0.0.1:9090",
			},
			SeeAlso: []string{
				"fuse help status",
				"fuse help tui",
			},
		}, true
	case "bench":
		return helpTopic{
			Name:    "bench",
			Summary: "Run local benchmark helpers and print the result as JSON.",
			Usage: []string{
				"fuse bench [flags]",
			},
			Notes: []string{
				"This is local-machine oriented and does not use the standard client connection flags.",
			},
			Flags: []helpFlag{
				{Name: "--db PATH", Description: "sqlite database path"},
			},
			Examples: []string{
				"./fuse bench",
				"./fuse bench --db .fuse/state.db",
			},
			SeeAlso: []string{
				"fuse help server",
			},
		}, true
	default:
		return helpTopic{}, false
	}
}

func usage() {
	fmt.Println("Fuse")
	fmt.Println()
	fmt.Println("Fuse is a cluster CLI and TUI for inspecting capacity, launching GPU jobs, and debugging scheduler placement.")
	fmt.Println()
	printHelpPairs("Usage",
		helpPair{Label: "fuse", Description: "launch the dashboard using the default connection source"},
		helpPair{Label: "fuse --faker", Description: "launch the dashboard against the built-in fake cluster"},
		helpPair{Label: "fuse tui [connection flags]", Description: "launch the dashboard explicitly"},
		helpPair{Label: "fuse <command> [flags]", Description: "run a one-shot CLI command"},
		helpPair{Label: "fuse help <command>", Description: "show detailed help for one command, for example `fuse help run`"},
	)
	printHelpSection("Command ordering",
		"Put connection flags after the command name when you want a CLI command.",
		"Good:  fuse status --faker",
		"Good:  fuse jobs --addr http://127.0.0.1:9090",
		"TUI:   fuse --faker",
		"Avoid: fuse --faker status",
		"Leading flags with no command launch the TUI.",
	)
	printHelpPairs("First steps",
		helpPair{Label: "make build", Description: "build ./fuse and refresh the legacy ./.bin/fuse-live wrapper"},
		helpPair{Label: "./fuse --help", Description: "show this help from the repo-local binary"},
		helpPair{Label: "./fuse --faker", Description: "open the fake-cluster dashboard and learn the UI safely"},
		helpPair{Label: "./fuse status --faker", Description: "verify the CLI path without touching live infrastructure"},
		helpPair{Label: "./fuse help run", Description: "read the main ad-hoc job submission guide"},
		helpPair{Label: "make install", Description: "install fuse into ~/.local/bin (or PREFIX/bin)"},
	)
	printHelpPairs("Binary paths",
		helpPair{Label: "./fuse", Description: "canonical repo-local binary"},
		helpPair{Label: "fuse", Description: "installed binary after make install"},
		helpPair{Label: "./.bin/fuse-live", Description: "generated compatibility wrapper for older scripts"},
	)
	printHelpPairs("Connection modes",
		helpPair{Label: "--faker", Description: "use the built-in fake cluster; best place to learn the CLI"},
		helpPair{Label: "--addr URL", Description: "talk to a running Fuse HTTP server instead of direct mode"},
		helpPair{Label: "--ssh-host HOST", Description: "override the SSH login host used by direct live mode"},
		helpPair{Label: "--nvml", Description: "probe the local machine with NVIDIA discovery instead of SSH"},
		helpPair{Label: "default behavior", Description: "without `--addr`, `--faker`, or `--nvml`, Fuse falls back to direct live mode"},
	)
	printHelpFlags("Shared client flags", sharedConnectionFlags())
	printHelpPairs("Environment variables",
		helpPair{Label: "FUSE_DB", Description: "default sqlite path for direct or server-backed workflows"},
		helpPair{Label: "FUSE_ARTIFACTS_DIR", Description: "default artifacts root for generated files and recipes"},
		helpPair{Label: "FUSE_SSH_HOST", Description: "default SSH host for direct live mode"},
		helpPair{Label: "FUSE_GUARANTEED_GPUS", Description: "default guaranteed GPU quota for direct mode"},
	)
	printHelpSection("Inspect the cluster",
		"status        One-line cluster summary",
		"nodes         Nodes, GPU counts, switch placement, and health",
		"fabric        Fabric links and bandwidth",
		"teams         Team quotas and GPU hours",
		"jobs          Job list with state and node placement",
		"events        Recent scheduler events",
		"storage       Filesystem usage and capacity",
		"tui           Full-screen dashboard",
	)
	printHelpSection("Plan placement and topology",
		"topo          Probe placement for a job or ephemeral allocation",
		"shard         Estimate tensor, pipeline, and data parallel splits",
		"why           Explain the scheduler state of a job",
	)
	printHelpSection("Launch and manage jobs",
		"submit        Submit a raw JSON JobSpec",
		"run           Submit an arbitrary command",
		"train         Submit a built-in training recipe",
		"finetune      Submit the built-in fine-tune recipe",
		"logs          Fetch stdout or stderr for a job",
		"cancel        Cancel a job",
		"checkpoint    Trigger a checkpoint",
		"checkpoints   List checkpoints for a job",
	)
	printHelpSection("Development and simulation",
		"server        Run the local Fuse HTTP server",
		"simulate      Apply fake scheduler actions such as add-node or kill-node",
		"bench         Run local benchmark helpers",
	)
	printHelpSection("Examples",
		"./fuse --faker",
		"./fuse status --faker",
		"./fuse jobs --addr http://127.0.0.1:9090",
		"./fuse run --faker --name smoke --gpus 1 -- bash -lc 'nvidia-smi'",
		"./fuse train --faker --example makemore --steps 200",
		"./fuse logs --faker --job run-123",
		"./fuse topo --faker --job run-123",
		"./fuse help run",
	)
	printHelpSection("TUI discoverability",
		"q or ctrl+c   Quit",
		"tab           Move focus between panes",
		"j k arrows    Scroll the active pane",
		"g / G         Jump to the top or bottom of a list",
		"/ or :        Open the command bar",
		"r             Refresh immediately",
		"?             Toggle the inline help footer",
	)
	printHelpSection("Command bar examples",
		":nodes",
		":jobs",
		":events",
		":refresh",
		":help",
		":quit",
	)
	printHelpSection("More help",
		"fuse help status",
		"fuse help run",
		"fuse help train",
		"fuse help topo",
		"fuse help logs",
		"fuse help server",
	)
}
