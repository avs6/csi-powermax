package service

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	pmax "github.com/dell/csi-powermax/pmax"
	mock "github.com/dell/csi-powermax/pmax/mock"

	"github.com/DATA-DOG/godog"
	"github.com/DATA-DOG/godog/gherkin"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gofsutil"
	"github.com/dell/goiscsi"
	ptypes "github.com/golang/protobuf/ptypes"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"

	types "github.com/dell/csi-powermax/pmax/types/v90"
)

const (
	goodVolumeID               = "11111"
	goodVolumeName             = "vol1"
	altVolumeID                = "22222"
	altVolumeName              = "vol2"
	goodNodeID                 = "node1"
	altNodeID                  = "7E012974-3651-4DCB-9954-25975A3C3CDF"
	datafile                   = "test/tmp/datafile"
	datafile2                  = "test/tmp/datafile2"
	datadir                    = "test/tmp/datadir"
	datadir2                   = "test/tmp/datadir2"
	volume1                    = "CSIXX-Int409498632-000197900046-00501"
	volume2                    = "CSIXX-Int409498632-000197900046-00502"
	volume0                    = "CSI-notfound-000197900046-00500"
	nodePublishBlockDevice     = "sdc"
	nodePublishBlockDevicePath = "test/dev/sdc"
	nodePublishSymlinkDir      = "test/dev/disk/by-id"
	nodePublishPrivateDir      = "test/tmp"
	nodePublishWWN             = "60000970000197900046533030324538"
	nodePublishLUNID           = "3"
	iSCSIEtcDir                = "test/etc/iscsi"
	iSCSIEtcFile               = "initiatorname.iscsi"
	goodSnapID                 = "444-444"
	altSnapID                  = "555-555"
	defaultStorageGroup        = "DefaultStorageGroup"
	defaultInitiator           = "iqn.1993-08.org.debian:01:5ae293b352a2"
)

type feature struct {
	nGoRoutines int
	lastTime    time.Time
	server      *httptest.Server
	service     *service
	err         error // return from the preceeding call
	// replace this with the Unispher client
	adminClient                          pmax.Pmax
	system                               *interface{}
	getPluginInfoResponse                *csi.GetPluginInfoResponse
	getPluginCapabilitiesResponse        *csi.GetPluginCapabilitiesResponse
	probeResponse                        *csi.ProbeResponse
	createVolumeResponse                 *csi.CreateVolumeResponse
	publishVolumeResponse                *csi.ControllerPublishVolumeResponse
	unpublishVolumeResponse              *csi.ControllerUnpublishVolumeResponse
	nodeGetInfoResponse                  *csi.NodeGetInfoResponse
	nodeGetCapabilitiesResponse          *csi.NodeGetCapabilitiesResponse
	deleteVolumeResponse                 *csi.DeleteVolumeResponse
	getCapacityResponse                  *csi.GetCapacityResponse
	controllerGetCapabilitiesResponse    *csi.ControllerGetCapabilitiesResponse
	validateVolumeCapabilitiesResponse   *csi.ValidateVolumeCapabilitiesResponse
	createSnapshotResponse               *csi.CreateSnapshotResponse
	createVolumeRequest                  *csi.CreateVolumeRequest
	publishVolumeRequest                 *csi.ControllerPublishVolumeRequest
	unpublishVolumeRequest               *csi.ControllerUnpublishVolumeRequest
	deleteVolumeRequest                  *csi.DeleteVolumeRequest
	listVolumesRequest                   *csi.ListVolumesRequest
	listVolumesResponse                  *csi.ListVolumesResponse
	listSnapshotsRequest                 *csi.ListSnapshotsRequest
	listSnapshotsResponse                *csi.ListSnapshotsResponse
	getVolumeByIDResponse                *GetVolumeByIDResponse
	listedVolumeIDs                      map[string]bool
	listVolumesNextTokenCache            string
	noNodeID                             bool
	omitAccessMode, omitVolumeCapability bool
	wrongCapacity, wrongStoragePool      bool
	useAccessTypeMount                   bool
	capability                           *csi.VolumeCapability
	capabilities                         []*csi.VolumeCapability
	nodePublishVolumeRequest             *csi.NodePublishVolumeRequest
	createSnapshotRequest                *csi.CreateSnapshotRequest
	volumeIDList                         []string
	volumeNameToID                       map[string]string
	snapshotIndex                        int
	selectedPortGroup                    string
	sgID                                 string
	mvID                                 string
	hostID                               string
	volumeID                             string
	IQNs                                 []string
	host                                 *types.Host
	allowedArrays                        []string
	iscsiTargets                         []goiscsi.ISCSITarget
}

var inducedErrors struct {
	invalidSymID        bool
	invalidStoragePool  bool
	invalidServiceLevel bool
	rescanError         bool
	noDeviceWWNError    bool
	badVolumeIdentifier bool
	invalidVolumeID     bool
	noVolumeID          bool
	differentVolumeID   bool
	portGroupError      bool
	noSymID             bool
	noNodeName          bool
	noIQNs              bool
}

func (f *feature) checkGoRoutines(tag string) {
	goroutines := runtime.NumGoroutine()
	fmt.Printf("goroutines %s new %d old groutines %d\n", tag, goroutines, f.nGoRoutines)
	f.nGoRoutines = goroutines
}

func (f *feature) aPowerMaxService() error {
	// Print the duration of the last operation so we can tell which tests are slow
	now := time.Now()
	if f.lastTime.IsZero() {
		dur := now.Sub(testStartTime)
		fmt.Printf("startup time: %v\n", dur)
	} else {
		dur := now.Sub(f.lastTime)
		fmt.Printf("time for last op: %v\n", dur)
	}
	f.lastTime = now
	devDiskByIDPrefix = "test/dev/disk/by-id/wwn-0x"
	nodePublishSleepTime = 5 * time.Millisecond
	removeDeviceSleepTime = 5 * time.Millisecond
	maxBlockDevicesPerWWN = 3
	f.checkGoRoutines("start aPowerMaxService")
	// Save off the admin client and the system
	if f.service != nil && f.service.adminClient != nil {
		f.adminClient = f.service.adminClient
		f.system = f.service.system
	}
	// Let the real code initialize it the first time, we reset the cache each test
	if pmaxCache != nil {
		pmaxCache = make(map[string]*pmaxCachedInformation)
	}
	f.err = nil
	f.getPluginInfoResponse = nil
	f.getPluginCapabilitiesResponse = nil
	f.probeResponse = nil
	f.createVolumeResponse = nil
	f.nodeGetInfoResponse = nil
	f.nodeGetCapabilitiesResponse = nil
	f.getCapacityResponse = nil
	f.controllerGetCapabilitiesResponse = nil
	f.validateVolumeCapabilitiesResponse = nil
	f.service = nil
	f.createVolumeRequest = nil
	f.publishVolumeRequest = nil
	f.unpublishVolumeRequest = nil
	f.noNodeID = false
	f.omitAccessMode = false
	f.omitVolumeCapability = false
	f.useAccessTypeMount = false
	f.wrongCapacity = false
	f.wrongStoragePool = false
	f.deleteVolumeRequest = nil
	f.deleteVolumeResponse = nil
	f.listVolumesRequest = nil
	f.listVolumesResponse = nil
	f.listVolumesNextTokenCache = ""
	f.listSnapshotsRequest = nil
	f.listSnapshotsResponse = nil
	f.listedVolumeIDs = make(map[string]bool)
	f.capability = nil
	f.capabilities = make([]*csi.VolumeCapability, 0)
	f.nodePublishVolumeRequest = nil
	f.createSnapshotRequest = nil
	f.createSnapshotResponse = nil
	f.volumeIDList = f.volumeIDList[:0]
	f.sgID = ""
	f.mvID = ""
	f.hostID = ""
	f.volumeNameToID = make(map[string]string)
	f.snapshotIndex = 0
	f.allowedArrays = []string{}
	if f.adminClient != nil {
		f.adminClient.SetAllowedArrays(f.allowedArrays)
	}
	f.iscsiTargets = make([]goiscsi.ISCSITarget, 0)

	inducedErrors.invalidSymID = false
	inducedErrors.invalidStoragePool = false
	inducedErrors.invalidServiceLevel = false
	inducedErrors.rescanError = false
	inducedErrors.noDeviceWWNError = false
	inducedErrors.badVolumeIdentifier = false
	inducedErrors.invalidVolumeID = false
	inducedErrors.noVolumeID = false
	inducedErrors.differentVolumeID = false
	inducedErrors.portGroupError = false
	inducedErrors.noSymID = false
	inducedErrors.noNodeName = false
	inducedErrors.noIQNs = false

	// configure gofsutil; we use a mock interface
	gofsutil.UseMockFS()
	gofsutil.GOFSMock.InduceBindMountError = false
	gofsutil.GOFSMock.InduceMountError = false
	gofsutil.GOFSMock.InduceGetMountsError = false
	gofsutil.GOFSMock.InduceDevMountsError = false
	gofsutil.GOFSMock.InduceUnmountError = false
	gofsutil.GOFSMock.InduceFormatError = false
	gofsutil.GOFSMock.InduceGetDiskFormatError = false
	gofsutil.GOFSMock.InduceWWNToDevicePathError = false
	gofsutil.GOFSMock.InduceRemoveBlockDeviceError = false
	gofsutil.GOFSMock.InduceGetDiskFormatType = ""
	gofsutil.GOFSMockMounts = gofsutil.GOFSMockMounts[:0]

	// configure variables in the driver
	getMappedVolMaxRetry = 1

	// Get or reuse the cached service
	f.getService()

	// create the mock iscsi client
	f.service.iscsiClient = goiscsi.NewMockISCSI(map[string]string{})
	goiscsi.GOISCSIMock.InduceDiscoveryError = false
	goiscsi.GOISCSIMock.InduceInitiatorError = false
	goiscsi.GOISCSIMock.InduceLoginError = false
	goiscsi.GOISCSIMock.InduceLogoutError = false
	goiscsi.GOISCSIMock.InduceRescanError = false

	// Get the httptest mock handler. Only set
	// a new server if there isn't one already.
	handler := mock.GetHandler()
	if handler != nil {
		if f.server == nil {
			f.server = httptest.NewServer(handler)
		}
		f.service.opts.Endpoint = f.server.URL
		log.Printf("server url: %s\n", f.server.URL)
	} else {
		f.server = nil
	}

	// Make sure the deletion worker is started.
	f.service.startDeletionWorker()
	f.checkGoRoutines("end aPowerMaxService")
	return nil
}

func (f *feature) getService() *service {
	mock.InducedErrors.NoConnection = false
	svc := new(service)
	if f.adminClient != nil {
		svc.adminClient = f.adminClient
	}
	if f.system != nil {
		svc.system = f.system
	}
	mock.Reset()
	mock.Data.JSONDir = "../pmax/mock"
	svc.loggedInArrays = map[string]bool{}

	var opts Opts
	opts.User = "username"
	opts.Password = "password"
	opts.SystemName = "14dbbf5617523654"
	opts.NodeName = "Node1"
	opts.Insecure = true
	opts.DisableCerts = true
	opts.PortGroups = []string{"portgroup1", "portgroup2"}
	opts.AllowedArrays = []string{}
	opts.EnableSnapshotCGDelete = true
	opts.EnableListVolumesSnapshots = true
	opts.ClusterPrefix = "TST"
	opts.Lsmod = `
Module                  Size  Used by
vsock_diag             12610  0
scini                 799210  0
ip6t_rpfilter          12595  1
`
	svc.opts = opts
	f.service = svc
	return svc
}

// GetPluginInfo
func (f *feature) iCallGetPluginInfo() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.GetPluginInfoRequest)
	f.getPluginInfoResponse, f.err = f.service.GetPluginInfo(ctx, req)
	if f.err != nil {
		return f.err
	}
	return nil
}
func (f *feature) aValidGetPluginInfoResponseIsReturned() error {
	rep := f.getPluginInfoResponse
	url := rep.GetManifest()["url"]
	if rep.GetName() == "" || rep.GetVendorVersion() == "" || url == "" {
		return errors.New("Expected GetPluginInfo to return name and version")
	}
	log.Printf("Name %s Version %s URL %s", rep.GetName(), rep.GetVendorVersion(), url)
	return nil
}

func (f *feature) iCallGetPluginCapabilities() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.GetPluginCapabilitiesRequest)
	f.getPluginCapabilitiesResponse, f.err = f.service.GetPluginCapabilities(ctx, req)
	if f.err != nil {
		return f.err
	}
	return nil
}

func (f *feature) aValidGetPluginCapabilitiesResponseIsReturned() error {
	rep := f.getPluginCapabilitiesResponse
	capabilities := rep.GetCapabilities()
	var foundController bool
	for _, capability := range capabilities {
		if capability.GetService().GetType() == csi.PluginCapability_Service_CONTROLLER_SERVICE {
			foundController = true
		}
	}
	if !foundController {
		return errors.New("Expected PlugiinCapabilitiesResponse to contain CONTROLLER_SERVICE")
	}
	return nil
}

func (f *feature) iCallProbe() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.ProbeRequest)
	f.checkGoRoutines("before probe")
	f.probeResponse, f.err = f.service.Probe(ctx, req)
	f.checkGoRoutines("after probe")
	return nil
}

func (f *feature) aValidProbeResponseIsReturned() error {
	if f.probeResponse.GetReady().GetValue() != true {
		return errors.New("Probe returned Ready false")
	}
	return nil
}

func (f *feature) theErrorContains(arg1 string) error {
	f.checkGoRoutines("theErrorContains")
	// If arg1 is none, we expect no error, any error received is unexpected
	if arg1 == "none" {
		if f.err == nil {
			return nil
		}
		return fmt.Errorf("Unexpected error: %s", f.err)
	}
	// We expected an error... unless there is a none clause
	if f.err == nil {
		// Check to see if no error is allowed as alternative
		possibleMatches := strings.Split(arg1, "@@")
		for _, possibleMatch := range possibleMatches {
			if possibleMatch == "none" {
				return nil
			}
		}
		return fmt.Errorf("Expected error to contain %s but no error", arg1)
	}
	// Allow for multiple possible matches, separated by @@. This was necessary
	// because Windows and Linux sometimes return different error strings for
	// gofsutil operations. Note @@ was used instead of || because the Gherkin
	// parser is not smart enough to ignore vertical braces within a quoted string,
	// so if || is used it thinks the row's cell count is wrong.
	possibleMatches := strings.Split(arg1, "@@")
	for _, possibleMatch := range possibleMatches {
		if strings.Contains(f.err.Error(), possibleMatch) {
			return nil
		}
	}
	return fmt.Errorf("Expected error to contain %s but it was %s", arg1, f.err.Error())
}

func (f *feature) thePossibleErrorContains(arg1 string) error {
	if f.err == nil {
		return nil
	}
	return f.theErrorContains(arg1)
}

func (f *feature) theControllerHasNoConnection() error {
	mock.InducedErrors.NoConnection = true
	return nil
}

func (f *feature) thereIsANodeProbeLsmodError() error {
	f.service.opts.Lsmod = ""
	return nil
}

func getTypicalCreateVolumeRequest() *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params[SymmetrixIDParam] = mock.DefaultSymmetrixID
	params[ServiceLevelParam] = mock.DefaultServiceLevel
	params[StoragePoolParam] = mock.DefaultStoragePool
	if inducedErrors.invalidSymID {
		params[SymmetrixIDParam] = ""
	}
	if inducedErrors.invalidServiceLevel {
		params[ServiceLevelParam] = "invalid"
	}
	if inducedErrors.invalidStoragePool {
		params[StoragePoolParam] = "invalid"
	}
	req.Parameters = params
	req.Name = "volume1"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 100 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	block := new(csi.VolumeCapability_BlockVolume)
	capability := new(csi.VolumeCapability)
	accessType := new(csi.VolumeCapability_Block)
	accessType.Block = block
	capability.AccessType = accessType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	return req
}

func (f *feature) iSpecifyCreateVolumeMountRequest(fstype string) error {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagepool"] = "viki_pool_HDD_20181031"
	params[SymmetrixIDParam] = mock.DefaultSymmetrixID
	params[ServiceLevelParam] = mock.DefaultServiceLevel
	params[StoragePoolParam] = mock.DefaultStoragePool
	req.Parameters = params
	req.Name = "mount1"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 8 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	capability := new(csi.VolumeCapability)
	mountVolume := new(csi.VolumeCapability_MountVolume)
	mountVolume.FsType = fstype
	mountVolume.MountFlags = make([]string, 0)
	mount := new(csi.VolumeCapability_Mount)
	mount.Mount = mountVolume
	capability.AccessType = mount
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iCallCreateVolume(name string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	if f.createVolumeRequest == nil {
		req := getTypicalCreateVolumeRequest()
		f.createVolumeRequest = req
	}
	req := f.createVolumeRequest
	req.Name = name

	f.createVolumeResponse, f.err = f.service.CreateVolume(ctx, req)
	if f.err != nil {
		log.Printf("CreateVolume called failed: %s\n", f.err.Error())
	}
	if f.createVolumeResponse != nil {
		log.Printf("vol id %s\n", f.createVolumeResponse.GetVolume().VolumeId)
		f.volumeID = f.createVolumeResponse.GetVolume().VolumeId
		f.volumeNameToID[name] = f.volumeID
	}
	return nil
}

func (f *feature) aValidCreateVolumeResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	if f.createVolumeResponse == nil || f.createVolumeResponse.Volume == nil {
		return errors.New("Expected a valid createVolumeResponse")
	}
	// Verify the Volume context
	params := f.createVolumeRequest.Parameters
	volumeContext := f.createVolumeResponse.GetVolume().VolumeContext
	fmt.Printf("volume:\n%#v\n", volumeContext)
	if params[StoragePoolParam] != volumeContext[StoragePoolParam] {
		return errors.New("StoragePoolParam in response should match the request")
	}
	if serviceLevel, ok := params[ServiceLevelParam]; ok {
		if serviceLevel != volumeContext[ServiceLevelParam] {
			return errors.New("ServiceLevelParam in response should match the request")
		}
	} else {
		if volumeContext[StoragePoolParam] != "Optimized" {
			return errors.New("ServiceLevelParam in response should be Optimized")
		}
	}
	f.volumeIDList = append(f.volumeIDList, f.createVolumeResponse.Volume.VolumeId)
	fmt.Printf("Service Level %s SRP %s\n",
		f.createVolumeResponse.Volume.VolumeContext[ServiceLevelParam],
		f.createVolumeResponse.Volume.VolumeContext[StoragePoolParam])
	return nil
}

func (f *feature) iSpecifyAccessibilityRequirements() error {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params[SymmetrixIDParam] = mock.DefaultSymmetrixID
	params[ServiceLevelParam] = mock.DefaultServiceLevel
	params[StoragePoolParam] = mock.DefaultStoragePool
	req.Parameters = params
	req.Name = "accessability"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 8 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	req.AccessibilityRequirements = new(csi.TopologyRequirement)
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyVolumeContentSource() error {
	req := getTypicalCreateVolumeRequest()
	req.Name = "volume_content_source"
	req.VolumeContentSource = new(csi.VolumeContentSource)
	req.VolumeContentSource.Type = &csi.VolumeContentSource_Volume{Volume: &csi.VolumeContentSource_VolumeSource{}}
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyMULTINODEWRITER() error {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params[SymmetrixIDParam] = mock.DefaultSymmetrixID
	params[ServiceLevelParam] = mock.DefaultServiceLevel
	params[StoragePoolParam] = mock.DefaultStoragePool
	req.Parameters = params
	req.Name = "multinode_writer"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 8 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	block := new(csi.VolumeCapability_BlockVolume)
	capability := new(csi.VolumeCapability)
	accessType := new(csi.VolumeCapability_Block)
	accessType.Block = block
	capability.AccessType = new(csi.VolumeCapability_Block)
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyABadCapacity() error {
	req := getTypicalCreateVolumeRequest()
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = -8 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	req.Name = "bad capacity"
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyAApplicationPrefix() error {
	req := getTypicalCreateVolumeRequest()
	params := req.GetParameters()
	params["ApplicationPrefix"] = "UNI"
	req.Parameters = params
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyAStorageGroup() error {
	req := getTypicalCreateVolumeRequest()
	params := req.GetParameters()
	params["StorageGroup"] = "UnitTestSG"
	req.Parameters = params
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyNoStoragePool() error {
	req := getTypicalCreateVolumeRequest()
	req.Parameters = nil
	req.Name = "no storage pool"
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iCallCreateVolumeSize(name string, size int64) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := getTypicalCreateVolumeRequest()
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = size * 1024 * 1024
	req.CapacityRange = capacityRange
	req.Name = name
	f.createVolumeRequest = req

	f.createVolumeResponse, f.err = f.service.CreateVolume(ctx, req)
	if f.err != nil {
		log.Printf("CreateVolumeSize called failed: %s\n", f.err.Error())
	}
	if f.createVolumeResponse != nil {
		log.Printf("vol id %s\n", f.createVolumeResponse.GetVolume().VolumeId)
		f.volumeID = f.createVolumeResponse.GetVolume().VolumeId
		f.volumeNameToID[name] = f.volumeID
	}
	return nil
}

func (f *feature) iChangeTheStoragePool(storagePoolName string) error {
	params := make(map[string]string)
	params[SymmetrixIDParam] = mock.DefaultSymmetrixID
	params[ServiceLevelParam] = "Diamond"
	params[StoragePoolParam] = mock.DefaultStoragePool
	f.createVolumeRequest.Parameters = params
	return nil
}

func (f *feature) iInduceError(errtype string) error {
	log.Printf("set induce error %s\n", errtype)
	switch errtype {
	case "InvalidSymID":
		inducedErrors.invalidSymID = true
	case "InvalidStoragePool":
		inducedErrors.invalidStoragePool = true
	case "InvalidServiceLevel":
		inducedErrors.invalidServiceLevel = true
	case "NoDeviceWWNError":
		inducedErrors.noDeviceWWNError = true
	case "PortGroupError":
		inducedErrors.portGroupError = true
	case "GetVolumeIteratorError":
		mock.InducedErrors.GetVolumeIteratorError = true
	case "GetVolumeError":
		mock.InducedErrors.GetVolumeError = true
	case "UpdateVolumeError":
		mock.InducedErrors.UpdateVolumeError = true
	case "DeleteVolumeError":
		mock.InducedErrors.DeleteVolumeError = true
	case "GetJobError":
		mock.InducedErrors.GetJobError = true
	case "JobFailedError":
		mock.InducedErrors.JobFailedError = true
	case "UpdateStorageGroupError":
		mock.InducedErrors.UpdateStorageGroupError = true
	case "GetStorageGroupError":
		mock.InducedErrors.GetStorageGroupError = true
	case "CreateStorageGroupError":
		mock.InducedErrors.CreateStorageGroupError = true
	case "CreateMaskingViewError":
		mock.InducedErrors.CreateMaskingViewError = true
	case "GetStoragePoolListError":
		mock.InducedErrors.GetStoragePoolListError = true
	case "GetHostError":
		mock.InducedErrors.GetHostError = true
	case "CreateHostError":
		mock.InducedErrors.CreateHostError = true
	case "UpdateHostError":
		mock.InducedErrors.UpdateHostError = true
	case "GetSymmetrixError":
		mock.InducedErrors.GetSymmetrixError = true
	case "GetStoragePoolError":
		mock.InducedErrors.GetStoragePoolError = true
	case "DeleteMaskingViewError":
		mock.InducedErrors.DeleteMaskingViewError = true
	case "GetMaskingViewConnectionsError":
		mock.InducedErrors.GetMaskingViewConnectionsError = true
	case "DeleteStorageGroupError":
		mock.InducedErrors.DeleteStorageGroupError = true
	case "GetPortGroupError":
		mock.InducedErrors.GetPortGroupError = true
	case "GetPortError":
		mock.InducedErrors.GetPortError = true
	case "GetDirectorError":
		mock.InducedErrors.GetDirectorError = true
	case "ResetAfterFirstError":
		mock.InducedErrors.ResetAfterFirstError = true
	case "NoSymlinkForNodePublish":
		cmd := exec.Command("rm", "-rf", nodePublishSymlinkDir)
		_, err := cmd.CombinedOutput()
		if err != nil {
			return err
		}
	case "NoBlockDevForNodePublish":
		unitTestEmulateBlockDevice = false
		cmd := exec.Command("rm", nodePublishBlockDevicePath)
		_, err := cmd.CombinedOutput()
		if err != nil {
			return nil
		}
	case "TargetNotCreatedForNodePublish":
		err := os.Remove(datafile)
		if err != nil {
			return nil
		}
		cmd := exec.Command("rm", "-rf", datadir)
		_, err = cmd.CombinedOutput()
		if err != nil {
			return err
		}
	case "PrivateDirectoryNotExistForNodePublish":
		f.service.privDir = "xxx/yyy"
	case "BlockMkfilePrivateDirectoryNodePublish":
		f.service.privDir = datafile
	case "NodePublishNoVolumeCapability":
		f.nodePublishVolumeRequest.VolumeCapability = nil
	case "NodePublishNoAccessMode":
		f.nodePublishVolumeRequest.VolumeCapability.AccessMode = nil
	case "NodePublishNoAccessType":
		f.nodePublishVolumeRequest.VolumeCapability.AccessType = nil
	case "NodePublishNoTargetPath":
		f.nodePublishVolumeRequest.TargetPath = ""
	case "NodePublishBlockTargetNotFile":
		f.nodePublishVolumeRequest.TargetPath = datadir
	case "NodePublishFileTargetNotDir":
		f.nodePublishVolumeRequest.TargetPath = datafile
	case "GOFSMockBindMountError":
		gofsutil.GOFSMock.InduceBindMountError = true
	case "GOFSMockDevMountsError":
		gofsutil.GOFSMock.InduceDevMountsError = true
	case "GOFSMockMountError":
		gofsutil.GOFSMock.InduceMountError = true
	case "GOFSMockGetMountsError":
		gofsutil.GOFSMock.InduceGetMountsError = true
	case "GOFSMockUnmountError":
		gofsutil.GOFSMock.InduceUnmountError = true
	case "GOFSMockGetDiskFormatError":
		gofsutil.GOFSMock.InduceGetDiskFormatError = true
	case "GOFSMockGetDiskFormatType":
		gofsutil.GOFSMock.InduceGetDiskFormatType = "unknown-fs"
	case "GOFSMockFormatError":
		gofsutil.GOFSMock.InduceFormatError = true
	case "GOFSWWNToDevicePathError":
		gofsutil.GOFSMock.InduceWWNToDevicePathError = true
	case "GOFSRmoveBlockDeviceError":
		gofsutil.GOFSMock.InduceRemoveBlockDeviceError = true
	case "GOISCSIDiscoveryError":
		goiscsi.GOISCSIMock.InduceDiscoveryError = true
	case "GOISCSIRescanError":
		goiscsi.GOISCSIMock.InduceRescanError = true
	case "NodeUnpublishNoTargetPath":
		f.nodePublishVolumeRequest.TargetPath = ""
	case "NodeUnpublishBadVolume":
		f.nodePublishVolumeRequest.VolumeId = volume0
	case "BadVolumeIdentifier":
		inducedErrors.badVolumeIdentifier = true
	case "InvalidVolumeID":
		inducedErrors.invalidVolumeID = true
	case "NoVolumeID":
		inducedErrors.noVolumeID = true
	case "DifferentVolumeID":
		inducedErrors.differentVolumeID = true
	case "UnspecifiedNodeName":
		f.service.opts.NodeName = ""
	case "NoArray":
		inducedErrors.noSymID = true
	case "NoNodeName":
		inducedErrors.noNodeName = true
	case "NoIQNs":
		inducedErrors.noIQNs = true
	case "none":
		return nil
	default:
		return fmt.Errorf("Don't know how to induce error %q", errtype)
	}
	return nil
}

func (f *feature) getControllerPublishVolumeRequest(accessType, nodeID string) *csi.ControllerPublishVolumeRequest {
	capability := new(csi.VolumeCapability)
	block := new(csi.VolumeCapability_Block)
	block.Block = new(csi.VolumeCapability_BlockVolume)
	if f.useAccessTypeMount {
		mountVolume := new(csi.VolumeCapability_MountVolume)
		mountVolume.FsType = "xfs"
		mountVolume.MountFlags = make([]string, 0)
		mount := new(csi.VolumeCapability_Mount)
		mount.Mount = mountVolume
		capability.AccessType = mount
	} else {
		capability.AccessType = block
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	switch accessType {
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
		break
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
		break
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
		break
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
		break
	}
	if !f.omitAccessMode {
		capability.AccessMode = accessMode
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.ControllerPublishVolumeRequest)
	if !inducedErrors.noVolumeID {
		if inducedErrors.invalidVolumeID || f.createVolumeResponse == nil {
			req.VolumeId = "000-000"
		} else {
			req.VolumeId = f.volumeID
		}
	}
	if !f.noNodeID {
		req.NodeId = nodeID
	}
	req.Readonly = false
	if !f.omitVolumeCapability {
		req.VolumeCapability = capability
	}
	// add in the context
	attributes := map[string]string{}
	attributes[StoragePoolParam] = mock.DefaultStoragePool
	attributes[ServiceLevelParam] = "Bronze"
	req.VolumeContext = attributes
	return req
}

func (f *feature) getControllerListVolumesRequest(maxEntries int32, startingToken string) *csi.ListVolumesRequest {
	return &csi.ListVolumesRequest{
		MaxEntries:    maxEntries,
		StartingToken: startingToken,
	}
}

func (f *feature) getControllerDeleteVolumeRequest(accessType string) *csi.DeleteVolumeRequest {
	capability := new(csi.VolumeCapability)
	block := new(csi.VolumeCapability_Block)
	block.Block = new(csi.VolumeCapability_BlockVolume)
	if f.useAccessTypeMount {
		mountVolume := new(csi.VolumeCapability_MountVolume)
		mountVolume.FsType = "xfs"
		mountVolume.MountFlags = make([]string, 0)
		mount := new(csi.VolumeCapability_Mount)
		mount.Mount = mountVolume
		capability.AccessType = mount
	} else {
		capability.AccessType = block
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	switch accessType {
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
		break
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
		break
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
		break
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
		break
	}
	if !f.omitAccessMode {
		capability.AccessMode = accessMode
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.DeleteVolumeRequest)
	if !inducedErrors.noVolumeID {
		if inducedErrors.invalidVolumeID {
			req.VolumeId = f.service.createCSIVolumeID(f.service.getClusterPrefix(), goodVolumeName, mock.DefaultSymmetrixID, "99999")
		} else {
			if f.volumeID != "" {
				req.VolumeId = f.volumeID
			} else {
				req.VolumeId = f.service.createCSIVolumeID(f.service.getClusterPrefix(), goodVolumeName, mock.DefaultSymmetrixID, goodVolumeID)
			}
		}
	}
	return req
}

func (f *feature) iHaveANodeWithMaskingView(nodeID string) error {
	f.service.opts.NodeName = nodeID
	f.hostID, f.sgID, f.mvID = f.service.GetHostSGAndMVIDFromNodeID(nodeID)
	initiators := []string{"iqn.1993-08.org.debian:01:5ae293b352a5"}
	mock.AddHost(f.hostID, "ISCSI", initiators)
	mock.AddStorageGroup(f.sgID, "", "")
	portGroupID := ""
	if f.selectedPortGroup != "" {
		portGroupID = f.selectedPortGroup
	} else {
		portGroupID = "iscsi_ports"
	}
	mock.AddMaskingView(f.mvID, f.sgID, f.hostID, portGroupID)
	return nil
}

func (f *feature) iHaveANodeWithHost(nodeID string) error {
	f.hostID, _, _ = f.service.GetHostSGAndMVIDFromNodeID(nodeID)
	initiators := []string{"iqn.1993-08.org.debian:01:5ae293b352a5"}
	mock.AddHost(f.hostID, "ISCSI", initiators)
	return nil
}

func (f *feature) iHaveANodeWithStorageGroup(nodeID string) error {
	_, f.sgID, _ = f.service.GetHostSGAndMVIDFromNodeID(nodeID)
	mock.AddStorageGroup(f.sgID, "", "")
	return nil
}

func (f *feature) iHaveANodeWithAFastManagedMaskingView(nodeID string) error {
	f.hostID, _, f.mvID = f.service.GetHostSGAndMVIDFromNodeID(nodeID)
	f.sgID = nodeID + "-Diamond-SRP_1-SG"
	initiators := []string{"iqn.1993-08.org.debian:01:5ae293b352a5"}
	mock.AddHost(f.hostID, "ISCSI", initiators)
	mock.AddStorageGroup(f.sgID, "SRP_1", "Diamond")
	mock.AddMaskingView(f.mvID, f.sgID, f.hostID, f.selectedPortGroup)
	return nil
}

func (f *feature) iHaveANodeWithFastManagedStorageGroup(nodeID string) error {
	_, f.sgID, _ = f.service.GetHostSGAndMVIDFromNodeID(nodeID)
	mock.AddStorageGroup(f.sgID, "SRP_1", "Diamond")
	return nil
}

func (f *feature) iAddTheVolumeTo(nodeID string) error {
	volumeIdentifier, _, devID, _ := f.service.parseCSIVolumeID(f.volumeID)
	mock.AddOneVolumeToStorageGroup(devID, volumeIdentifier, f.sgID, 1)
	return nil
}

func (f *feature) iCallPublishVolumeWithTo(accessMode, nodeID string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.publishVolumeRequest
	if f.publishVolumeRequest == nil {
		req = f.getControllerPublishVolumeRequest(accessMode, nodeID)
		f.publishVolumeRequest = req
	}
	log.Printf("Calling controllerPublishVolume")
	f.publishVolumeResponse, f.err = f.service.ControllerPublishVolume(ctx, req)
	if f.err != nil {
		log.Printf("PublishVolume call failed: %s\n", f.err.Error())
	}
	f.publishVolumeRequest = nil
	return nil
}

func (f *feature) aValidPublishVolumeResponseIsReturned() error {
	if f.err != nil {
		return errors.New("PublishVolume returned error: " + f.err.Error())
	}
	if f.publishVolumeResponse == nil {
		return errors.New("No PublishVolumeResponse returned")
	}
	for key, value := range f.publishVolumeResponse.PublishContext {
		fmt.Printf("PublishContext %s: %s", key, value)
	}
	return nil
}

func (f *feature) aValidVolume() error {
	devID := goodVolumeID
	volumeIdentifier := csiPrefix + f.service.getClusterPrefix() + "-" + goodVolumeName
	sgList := make([]string, 1)
	sgList[0] = defaultStorageGroup
	mock.AddStorageGroup(defaultStorageGroup, "SRP_1", "Optimized")
	mock.AddOneVolumeToStorageGroup(devID, volumeIdentifier, defaultStorageGroup, 1)
	f.volumeID = f.service.createCSIVolumeID(f.service.getClusterPrefix(), goodVolumeName, mock.DefaultSymmetrixID, goodVolumeID)
	return nil
}

func (f *feature) anInvalidVolume() error {
	inducedErrors.invalidVolumeID = true
	return nil
}

func (f *feature) noVolume() error {
	inducedErrors.noVolumeID = true
	return nil
}

func (f *feature) noNode() error {
	f.noNodeID = true
	return nil
}

func (f *feature) noVolumeCapability() error {
	f.omitVolumeCapability = true
	return nil
}

func (f *feature) noAccessMode() error {
	f.omitAccessMode = true
	return nil
}

func (f *feature) thenIUseADifferentNodeID() error {
	f.publishVolumeRequest.NodeId = altNodeID
	if f.unpublishVolumeRequest != nil {
		f.unpublishVolumeRequest.NodeId = altNodeID
	}
	return nil
}

func (f *feature) iUseAccessTypeMount() error {
	f.useAccessTypeMount = true
	return nil
}

func (f *feature) noErrorWasReceived() error {
	if f.err != nil {
		return f.err
	}
	return nil
}

func (f *feature) getControllerUnpublishVolumeRequest(nodeID string) *csi.ControllerUnpublishVolumeRequest {
	req := new(csi.ControllerUnpublishVolumeRequest)
	if !inducedErrors.noVolumeID {
		if inducedErrors.invalidVolumeID {
			req.VolumeId = "9999-9999"
		} else {
			if !f.noNodeID {
				req.VolumeId = f.volumeID
			}
		}
	}
	if !f.noNodeID {
		req.NodeId = nodeID
	}
	return req
}

func (f *feature) iCallUnpublishVolumeFrom(nodeID string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.unpublishVolumeRequest
	if f.unpublishVolumeRequest == nil {
		req = f.getControllerUnpublishVolumeRequest(nodeID)
		f.unpublishVolumeRequest = req
	}
	log.Printf("Calling controllerUnpublishVolume: %s", req.VolumeId)
	f.unpublishVolumeResponse, f.err = f.service.ControllerUnpublishVolume(ctx, req)
	if f.err != nil {
		log.Printf("UnpublishVolume call failed: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) aValidUnpublishVolumeResponseIsReturned() error {
	if f.unpublishVolumeResponse == nil {
		return errors.New("expected unpublishVolumeResponse (with no contents)but did not get one")
	}
	return nil
}

func (f *feature) iCallNodeGetInfo() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodeGetInfoRequest)
	f.nodeGetInfoResponse, f.err = f.service.NodeGetInfo(ctx, req)
	return nil
}

func (f *feature) aValidNodeGetInfoResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	if f.nodeGetInfoResponse.NodeId == "" {
		return errors.New("expected NodeGetInfoResponse to contain NodeID but it was null")
	}
	if f.nodeGetInfoResponse.MaxVolumesPerNode != 0 {
		return errors.New("expected NodeGetInfoResponse MaxVolumesPerNode to be 0")
	}
	fmt.Printf("NodeID %s\n", f.nodeGetInfoResponse.NodeId)
	return nil
}

func (f *feature) iCallDeleteVolumeWith(arg1 string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.deleteVolumeRequest
	if f.deleteVolumeRequest == nil {
		req = f.getControllerDeleteVolumeRequest(arg1)
		f.deleteVolumeRequest = req
	}
	log.Printf("Calling DeleteVolume")
	f.deleteVolumeResponse, f.err = f.service.DeleteVolume(ctx, req)
	if f.err != nil {
		log.Printf("DeleteVolume called failed: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) aValidDeleteVolumeResponseIsReturned() error {
	if f.deleteVolumeResponse == nil {
		return errors.New("expected deleteVolumeResponse (with no contents) but did not get one")
	}
	return nil
}

func (f *feature) aValidListVolumesResponseIsReturned() error {
	if f.listVolumesResponse == nil {
		return errors.New("expected a non-nil listVolumesResponse, but it was nil")
	}
	return nil
}

func getTypicalCapacityRequest(valid bool) *csi.GetCapacityRequest {
	req := new(csi.GetCapacityRequest)
	// Construct the volume capabilities
	capability := new(csi.VolumeCapability)
	// Set FS type to mount volume
	mount := new(csi.VolumeCapability_MountVolume)
	accessType := new(csi.VolumeCapability_Mount)
	accessType.Mount = mount
	capability.AccessType = accessType
	// A single mode writer
	accessMode := new(csi.VolumeCapability_AccessMode)
	if valid {
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	} else {
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	return req
}

func (f *feature) iCallGetCapacityWithStoragePool(srpID string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := getTypicalCapacityRequest(true)
	parameters := make(map[string]string)
	parameters[StoragePoolParam] = srpID
	parameters[SymmetrixIDParam] = mock.DefaultSymmetrixID
	req.Parameters = parameters

	fmt.Printf("Calling GetCapacity with %s and %s\n",
		req.Parameters[StoragePoolParam], req.Parameters[SymmetrixIDParam])
	f.getCapacityResponse, f.err = f.service.GetCapacity(ctx, req)
	if f.err != nil {
		log.Printf("GetCapacity call failed: %s\n", f.err.Error())
		return nil
	}
	return nil
}

func (f *feature) iCallGetCapacityWithoutSymmetrixID() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := getTypicalCapacityRequest(true)
	parameters := make(map[string]string)
	parameters[StoragePoolParam] = mock.DefaultStoragePool
	req.Parameters = parameters
	f.getCapacityResponse, f.err = f.service.GetCapacity(ctx, req)
	if f.err != nil {
		log.Printf("GetCapacity call failed: %s\n", f.err.Error())
		return nil
	}
	return nil
}

func (f *feature) iCallGetCapacityWithoutParameters() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := getTypicalCapacityRequest(true)
	req.Parameters = nil
	f.getCapacityResponse, f.err = f.service.GetCapacity(ctx, req)
	if f.err != nil {
		log.Printf("GetCapacity call failed: %s\n", f.err.Error())
		return nil
	}
	return nil
}

func (f *feature) iCallGetCapacityWithInvalidCapabilities() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := getTypicalCapacityRequest(false)
	f.getCapacityResponse, f.err = f.service.GetCapacity(ctx, req)
	if f.err != nil {
		log.Printf("GetCapacity call failed: %s\n", f.err.Error())
		return nil
	}
	return nil
}

func (f *feature) aValidGetCapacityResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	if f.getCapacityResponse == nil {
		return errors.New("Received null response to GetCapacity")
	}
	if f.getCapacityResponse.AvailableCapacity <= 0 {
		return errors.New("Expected AvailableCapacity to be positive")
	}
	fmt.Printf("Available capacity: %d\n", f.getCapacityResponse.AvailableCapacity)
	return nil
}

func (f *feature) iCallControllerGetCapabilities() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.ControllerGetCapabilitiesRequest)
	log.Printf("Calling ControllerGetCapabilities")
	f.controllerGetCapabilitiesResponse, f.err = f.service.ControllerGetCapabilities(ctx, req)
	if f.err != nil {
		log.Printf("ControllerGetCapabilities call failed: %s\n", f.err.Error())
		return f.err
	}
	return nil
}

// parseListVolumesTable parses the given DataTable and ensures that it follows the
// format:
// | max_entries | starting_token |
// | <number>    | <string>       |
func parseListVolumesTable(dt *gherkin.DataTable) (int32, string, error) {
	if c := len(dt.Rows); c != 2 {
		return 0, "", fmt.Errorf("expected table with header row and single value row, got %d row(s)", c)
	}

	var (
		maxEntries    int32
		startingToken string
	)
	for i, v := range dt.Rows[0].Cells {
		switch h := v.Value; h {
		case "max_entries":
			str := dt.Rows[1].Cells[i].Value
			n, err := strconv.Atoi(str)
			if err != nil {
				return 0, "", fmt.Errorf("expected a valid number for max_entries, got %v", err)
			}
			maxEntries = int32(n)
		case "starting_token":
			startingToken = dt.Rows[1].Cells[i].Value
		default:
			return 0, "", fmt.Errorf(`want headers ["max_entries", "starting_token"], got %q`, h)
		}
	}

	return maxEntries, startingToken, nil
}

// iCallListVolumesAgainWith nils out the previous request before delegating
// to iCallListVolumesWith with the same table data.  This simulates multiple
// calls to ListVolume for the purpose of testing the pagination token.
func (f *feature) iCallListVolumesAgainWith(dt *gherkin.DataTable) error {
	f.listVolumesRequest = nil
	return f.iCallListVolumesWith(dt)
}

func (f *feature) iCallListVolumesWith(dt *gherkin.DataTable) error {
	maxEntries, startingToken, err := parseListVolumesTable(dt)
	if err != nil {
		return err
	}

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.listVolumesRequest
	if f.listVolumesRequest == nil {
		switch st := startingToken; st {
		case "none":
			startingToken = ""
		case "next":
			startingToken = f.listVolumesNextTokenCache
		case "invalid":
			startingToken = "invalid-token"
		case "larger":
			startingToken = "9999"
		default:
			return fmt.Errorf(`want start token of "next", "none", "invalid", "larger", got %q`, st)
		}
		req = f.getControllerListVolumesRequest(maxEntries, startingToken)
		f.listVolumesRequest = req
	}
	log.Printf("Calling ListVolumes with req=%+v", f.listVolumesRequest)
	f.listVolumesResponse, f.err = f.service.ListVolumes(ctx, req)
	if f.err != nil {
		log.Printf("ListVolume called failed: %s\n", f.err.Error())
	} else if f.listVolumesResponse == nil {
		log.Printf("Received null response from ListVolumes")
	} else {
		f.listVolumesNextTokenCache = f.listVolumesResponse.NextToken
	}
	return nil
}

func (f *feature) aValidControllerGetCapabilitiesResponseIsReturned() error {
	rep := f.controllerGetCapabilitiesResponse
	if rep != nil {
		if rep.Capabilities == nil {
			return errors.New("no capabilities returned in ControllerGetCapabilitiesResponse")
		}
		count := 0
		for _, cap := range rep.Capabilities {
			typex := cap.GetRpc().Type
			switch typex {
			case csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_GET_CAPACITY:
				count = count + 1
			default:
				return fmt.Errorf("received unexpected capability: %v", typex)
			}
		}
		if count != 3 {
			return fmt.Errorf("Did not retrieve all the expected capabilities")
		}
		return nil
	}
	return fmt.Errorf("expected ControllerGetCapabilitiesResponse but didn't get one")
}

func (f *feature) iCallValidateVolumeCapabilitiesWithVoltypeAccessFstype(voltype, access, fstype, pool, level string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.ValidateVolumeCapabilitiesRequest)
	if inducedErrors.invalidVolumeID || f.volumeID == "" {
		req.VolumeId = "000-000"
	} else if inducedErrors.differentVolumeID {
		req.VolumeId = f.service.createCSIVolumeID(f.service.getClusterPrefix(), altVolumeName, mock.DefaultSymmetrixID, goodVolumeID)
	} else {
		req.VolumeId = f.volumeID
	}
	// Construct the volume capabilities
	capability := new(csi.VolumeCapability)
	switch voltype {
	case "block":
		block := new(csi.VolumeCapability_BlockVolume)
		accessType := new(csi.VolumeCapability_Block)
		accessType.Block = block
		capability.AccessType = accessType
	case "mount":
		mount := new(csi.VolumeCapability_MountVolume)
		accessType := new(csi.VolumeCapability_Mount)
		accessType.Mount = mount
		capability.AccessType = accessType
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	switch access {
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multi-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "multi-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multi-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
	}
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	// add in the context
	attributes := map[string]string{}
	if pool != "" {
		attributes[StoragePoolParam] = pool
	}
	if level != "" {
		attributes[ServiceLevelParam] = level
	}
	req.VolumeContext = attributes

	log.Printf("Calling ValidateVolumeCapabilities")
	f.validateVolumeCapabilitiesResponse, f.err = f.service.ValidateVolumeCapabilities(ctx, req)
	if f.err != nil || f.validateVolumeCapabilitiesResponse == nil {
		return nil
	}
	if f.validateVolumeCapabilitiesResponse.Message != "" {
		f.err = errors.New(f.validateVolumeCapabilitiesResponse.Message)
	} else {
		// Validate we get a Confirmed structure with VolumeCapabilities
		if f.validateVolumeCapabilitiesResponse.Confirmed == nil {
			return errors.New("Expected ValidateVolumeCapabilities to have a Confirmed structure but it did not")
		}
		confirmed := f.validateVolumeCapabilitiesResponse.Confirmed
		if len(confirmed.VolumeCapabilities) <= 0 {
			return errors.New("Expected ValidateVolumeCapabilities to return the confirmed VolumeCapabilities but it did not")
		}
	}
	return nil
}

// thereAreValidVolumes creates the requested number of volumes
// for the test scenario, using a suffix.
func (f *feature) thereAreValidVolumes(n int) error {
	idTemplate := "11111%d"
	nameTemplate := "vol%d"
	mock.AddStorageGroup(defaultStorageGroup, "SRP_1", "Diamond")
	for i := 0; i < n; i++ {
		name := fmt.Sprintf(nameTemplate, i)
		id := fmt.Sprintf(idTemplate, i)
		mock.AddOneVolumeToStorageGroup(id, name, defaultStorageGroup, 1)
	}
	return nil
}

func (f *feature) volumesAreListed(expected int) error {
	if f.listVolumesResponse == nil {
		return fmt.Errorf("expected a non-nil list volume response, but got nil")
	}

	if actual := len(f.listVolumesResponse.Entries); actual != expected {
		return fmt.Errorf("expected %d volumes to have been listed, got %d", expected, actual)
	}
	return nil
}

func (f *feature) anInvalidListVolumesResponseIsReturned() error {
	if f.err == nil {
		return fmt.Errorf("expected error response, but couldn't find it")
	}
	return nil
}

func (f *feature) aCapabilityWithVoltypeAccessFstype(voltype, access, fstype string) error {
	// Construct the volume capabilities
	capability := new(csi.VolumeCapability)
	switch voltype {
	case "block":
		blockVolume := new(csi.VolumeCapability_BlockVolume)
		block := new(csi.VolumeCapability_Block)
		block.Block = blockVolume
		capability.AccessType = block
	case "mount":
		mountVolume := new(csi.VolumeCapability_MountVolume)
		mountVolume.FsType = fstype
		mountVolume.MountFlags = make([]string, 0)
		mount := new(csi.VolumeCapability_Mount)
		mount.Mount = mountVolume
		capability.AccessType = mount
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	switch access {
	case "single-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multiple-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
	}
	capability.AccessMode = accessMode
	f.capabilities = make([]*csi.VolumeCapability, 0)
	f.capabilities = append(f.capabilities, capability)
	f.capability = capability
	return nil
}

func (f *feature) aControllerPublishedVolume() error {
	var err error
	fmt.Printf("setting up dev directory, block device, and symlink\n")

	// Make the directories; on Windows these show up in C:/dev/...
	_, err = os.Stat(nodePublishSymlinkDir)
	if err != nil {
		err = os.MkdirAll(nodePublishSymlinkDir, 0777)
		if err != nil {
			fmt.Printf("by-id: " + err.Error())
		}
	}

	// Remove the private staging directory
	cmd := exec.Command("rm", "-rf", nodePublishPrivateDir)
	_, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("error removing private staging directory")
	} else {
		fmt.Printf("removed private staging directory")
	}

	// Remake the private staging directory
	err = os.MkdirAll(nodePublishPrivateDir, 0777)
	if err != nil {
		fmt.Printf("error creating private staging directory: " + err.Error())
	}
	f.service.privDir = nodePublishPrivateDir

	// Make the block device
	_, err = os.Stat(nodePublishBlockDevicePath)
	if err != nil {
		fmt.Printf("stat error: %s\n", err.Error())
		cmd := exec.Command("mknod", nodePublishBlockDevicePath, "b", "0", "0")
		_, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("error creating device node\n")
		}
	}

	// Make the symlink
	symlinkString := fmt.Sprintf("wwn-0x%s", nodePublishWWN)
	_, err = os.Stat(nodePublishSymlinkDir + "/" + symlinkString)
	if err != nil {
		cmdstring := fmt.Sprintf("cd %s; ln -s ../../%s %s", nodePublishSymlinkDir, nodePublishBlockDevice, symlinkString)
		cmd = exec.Command("sh", "-c", cmdstring)
		output, err := cmd.CombinedOutput()
		fmt.Printf("symlink output: %s\n", output)
		if err != nil {
			fmt.Printf("link: " + err.Error())
		}
	}
	devDiskByIDPrefix = "test/dev/disk/by-id/wwn-0x"

	// Set the callback function
	gofsutil.GOFSRescanCallback = rescanCallback

	// Make the target directory if required
	_, err = os.Stat(datadir)
	if err != nil {
		err = os.MkdirAll(datadir, 0777)
		if err != nil {
			fmt.Printf("Couldn't make datadir: %s\n", datadir)
		}
	}

	// Make the target file if required
	_, err = os.Stat(datafile)
	if err != nil {
		file, err := os.Create(datafile)
		if err != nil {
			fmt.Printf("Couldn't make datafile: %s\n", datafile)
		} else {
			file.Close()
		}
	}

	// Empty WindowsMounts in gofsutil
	// gofsutil.GOFSMockMounts = gofsutil.GOFSMockMounts[:0]
	// Set variables in mount for unit testing
	unitTestEmulateBlockDevice = true
	return nil
}

func rescanCallback(scanstring string) {
	if gofsutil.GOFSMockWWNToDevice == nil {
		gofsutil.GOFSMockWWNToDevice = make(map[string]string)
	}
	switch scanstring {
	case "3":
		symlink := fmt.Sprintf("%s/wwn-0x%s", nodePublishSymlinkDir, nodePublishWWN)
		gofsutil.GOFSMockWWNToDevice[nodePublishWWN] = symlink
		fmt.Printf("gofsutilRescanCallback publishing %s to %s\n", nodePublishWWN, symlink)
	}
}

func (f *feature) getNodePublishVolumeRequest() error {
	req := new(csi.NodePublishVolumeRequest)
	req.VolumeId = volume1
	volName, _, devID, err := f.service.parseCSIVolumeID(volume1)
	if err != nil {
		return errors.New("couldn't parse volume1")
	}
	//mock.NewVolume(devID, volName, 1000, make([]string, 0))
	mock.AddOneVolumeToStorageGroup(devID, volName, f.sgID, 1000)
	req.Readonly = false
	req.VolumeCapability = f.capability
	req.PublishContext = make(map[string]string)
	req.PublishContext[PublishContextDeviceWWN] = nodePublishWWN
	req.PublishContext[PublishContextLUNAddress] = nodePublishLUNID
	block := f.capability.GetBlock()
	if block != nil {
		req.TargetPath = datafile
	}
	mount := f.capability.GetMount()
	if mount != nil {
		req.TargetPath = datadir
	}
	req.VolumeContext = make(map[string]string)
	req.VolumeContext["VolumeId"] = req.VolumeId
	f.nodePublishVolumeRequest = req
	return nil
}

func (f *feature) iChangeTheTargetPath() error {
	// Make the target directory if required
	_, err := os.Stat(datadir2)
	if err != nil {
		err = os.MkdirAll(datadir2, 0777)
		if err != nil {
			fmt.Printf("Couldn't make datadir: %s\n", datadir2)
		}
	}

	// Make the target file if required
	_, err = os.Stat(datafile2)
	if err != nil {
		file, err := os.Create(datafile2)
		if err != nil {
			fmt.Printf("Couldn't make datafile: %s\n", datafile2)
		} else {
			file.Close()
		}
	}
	req := f.nodePublishVolumeRequest
	block := f.capability.GetBlock()
	if block != nil {
		req.TargetPath = datafile2
	}
	mount := f.capability.GetMount()
	if mount != nil {
		req.TargetPath = datadir2
	}
	return nil
}

func (f *feature) iMarkRequestReadOnly() error {
	f.nodePublishVolumeRequest.Readonly = true
	return nil
}

func (f *feature) iCallNodePublishVolume() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.nodePublishVolumeRequest
	if inducedErrors.noDeviceWWNError {
		req.PublishContext[PublishContextDeviceWWN] = ""
	}
	if inducedErrors.badVolumeIdentifier {
		req.VolumeId = "bad volume identifier"
	}
	if req == nil {
		_ = f.getNodePublishVolumeRequest()
		req = f.nodePublishVolumeRequest
	}
	fmt.Printf("Calling NodePublishVolume\n")
	_, err := f.service.NodePublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("NodePublishVolume failed: %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	} else {
		fmt.Printf("NodePublishVolume completed successfully\n")
	}
	return nil
}

func (f *feature) iCallNodeUnpublishVolume() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodeUnpublishVolumeRequest)
	req.VolumeId = f.nodePublishVolumeRequest.VolumeId
	req.TargetPath = f.nodePublishVolumeRequest.TargetPath
	if inducedErrors.badVolumeIdentifier {
		req.VolumeId = "bad volume identifier"
	}
	fmt.Printf("Calling NodeUnpublishVolume\n")
	_, err := f.service.NodeUnpublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("NodeUnpublishVolume failed: %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	} else {
		fmt.Printf("NodeUnpublishVolume completed successfully\n")
	}
	return nil
}

func (f *feature) thereAreNoRemainingMounts() error {
	if len(gofsutil.GOFSMockMounts) > 0 {
		return errors.New("expected all mounts to be removed but one or more remained")
	}
	return nil
}

func (f *feature) getTypicalEnviron() []string {
	stringSlice := make([]string, 0)
	stringSlice = append(stringSlice, EnvEndpoint+"=unix_sock")
	stringSlice = append(stringSlice, EnvUser+"=admin")
	stringSlice = append(stringSlice, EnvPassword+"=password")
	stringSlice = append(stringSlice, EnvNodeName+"=Node1")
	stringSlice = append(stringSlice, EnvPortGroups+"=PortGroup1,PortGroup2")
	stringSlice = append(stringSlice, EnvArrayWhitelist+"=")
	stringSlice = append(stringSlice, EnvThick+"=bad")
	stringSlice = append(stringSlice, EnvInsecure+"=true")
	stringSlice = append(stringSlice, EnvGrpcMaxThreads+"=1")
	stringSlice = append(stringSlice, "X_CSI_PRIVATE_MOUNT_DIR=/csi")
	return stringSlice
}

func (f *feature) iCallBeforeServe() error {
	ctxOSEnviron := interface{}("os.Environ")
	stringSlice := f.getTypicalEnviron()
	stringSlice = append(stringSlice, EnvClusterPrefix+"=TST")
	ctx := context.WithValue(context.Background(), ctxOSEnviron, stringSlice)
	listener, err := net.Listen("tcp", "127.0.0.1:65000")
	if err != nil {
		return err
	}
	f.err = f.service.BeforeServe(ctx, nil, listener)
	listener.Close()
	return nil
}

func (f *feature) iCallBeforeServeWithoutClusterPrefix() error {
	ctxOSEnviron := interface{}("os.Environ")
	stringSlice := f.getTypicalEnviron()
	ctx := context.WithValue(context.Background(), ctxOSEnviron, stringSlice)
	listener, err := net.Listen("tcp", "127.0.0.1:65000")
	if err != nil {
		return err
	}
	f.err = f.service.BeforeServe(ctx, nil, listener)
	listener.Close()
	return nil
}

func (f *feature) iCallBeforeServeWithAnInvalidClusterPrefix() error {
	ctxOSEnviron := interface{}("os.Environ")
	stringSlice := f.getTypicalEnviron()
	stringSlice = append(stringSlice, EnvClusterPrefix+"=LONG")
	ctx := context.WithValue(context.Background(), ctxOSEnviron, stringSlice)
	listener, err := net.Listen("tcp", "127.0.0.1:65000")
	if err != nil {
		return err
	}
	f.err = f.service.BeforeServe(ctx, nil, listener)
	listener.Close()
	return nil
}

func (f *feature) iCallNodeStageVolume() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodeStageVolumeRequest)
	_, f.err = f.service.NodeStageVolume(ctx, req)
	return nil
}

func (f *feature) iCallNodeUnstageVolume() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodeUnstageVolumeRequest)
	_, f.err = f.service.NodeUnstageVolume(ctx, req)
	return nil
}

func (f *feature) iCallNodeGetCapabilities() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodeGetCapabilitiesRequest)
	f.nodeGetCapabilitiesResponse, f.err = f.service.NodeGetCapabilities(ctx, req)
	return nil
}

func (f *feature) aValidNodeGetCapabilitiesResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	if len(f.nodeGetCapabilitiesResponse.Capabilities) > 0 {
		return errors.New("expected NodeGetCapabilities to return no capabilities")
	}
	return nil
}

func (f *feature) iCallCreateSnapshotWith(snapName string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)

	if len(f.volumeIDList) == 0 {
		f.volumeIDList = append(f.volumeIDList, "00000000")
	}
	req := &csi.CreateSnapshotRequest{
		SourceVolumeId: f.volumeIDList[0],
		Name:           snapName,
	}
	if inducedErrors.invalidVolumeID {
		req.SourceVolumeId = "00000000"
	} else if inducedErrors.noVolumeID {
		req.SourceVolumeId = ""
	} else if len(f.volumeIDList) > 1 {
		req.Parameters = make(map[string]string)
		stringList := ""
		for _, v := range f.volumeIDList {
			if stringList == "" {
				stringList = v
			} else {
				stringList = stringList + "," + v
			}
		}
		// TODO
		// req.Parameters[VolumeIDList] = stringList
		return errors.New("VolumeIDList and snap cg not implemented")
	}
	f.createSnapshotResponse, f.err = f.service.CreateSnapshot(ctx, req)
	return nil
}

func (f *feature) aValidCreateSnapshotResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	if f.createSnapshotResponse == nil {
		return errors.New("Expected CreateSnapshotResponse to be returned")
	}
	return nil
}

func (f *feature) aValidSnapshot() error {
	return godog.ErrPending
}

func (f *feature) iCallDeleteSnapshot() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.DeleteSnapshotRequest{SnapshotId: goodSnapID, Secrets: make(map[string]string)}
	req.Secrets["x"] = "y"
	if inducedErrors.invalidVolumeID {
		req.SnapshotId = "00000000"
	} else if inducedErrors.noVolumeID {
		req.SnapshotId = ""
	}
	_, f.err = f.service.DeleteSnapshot(ctx, req)
	return nil
}

func (f *feature) aValidSnapshotConsistencyGroup() error {
	return godog.ErrPending
}

func (f *feature) iCallCreateVolumeFromSnapshot() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := getTypicalCreateVolumeRequest()
	req.Name = "volumeFromSnap"
	if f.wrongCapacity {
		req.CapacityRange.RequiredBytes = 64 * 1024 * 1024 * 1024
	}
	if f.wrongStoragePool {
		req.Parameters["storagepool"] = "bad storage pool"
	}
	source := &csi.VolumeContentSource_SnapshotSource{SnapshotId: goodSnapID}
	req.VolumeContentSource = new(csi.VolumeContentSource)
	req.VolumeContentSource.Type = &csi.VolumeContentSource_Snapshot{Snapshot: source}
	f.createVolumeResponse, f.err = f.service.CreateVolume(ctx, req)
	if f.err != nil {
		fmt.Printf("Error on CreateVolume from snap: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) theWrongCapacity() error {
	f.wrongCapacity = true
	return nil
}

func (f *feature) theWrongStoragePool() error {
	f.wrongStoragePool = true
	return nil
}

// Every increasing int used to generate unique snapshot indexes

func (f *feature) thereAreValidSnapshotsOfVolume(nsnapshots int, volume string) error {
	return godog.ErrPending
}

func (f *feature) iCallListSnapshotsWithMaxEntriesAndStartingToken(maxEntriesString, startingTokenString string) error {
	return godog.ErrPending
}

func (f *feature) iCallListSnapshotsForVolume(arg1 string) error {
	return godog.ErrPending
}

func (f *feature) iCallListSnapshotsForSnapshot(arg1 string) error {
	return godog.ErrPending
}

func (f *feature) theSnapshotIDIs(arg1 string) error {
	if len(f.listedVolumeIDs) != 1 {
		return errors.New("Expected only 1 volume to be listed")
	}
	if f.listedVolumeIDs[arg1] == false {
		return errors.New("Expected volume was not found")
	}
	return nil
}

func (f *feature) aValidListSnapshotsResponseIsReturnedWithListedAndNextToken(listed, nextTokenString string) error {
	if f.err != nil {
		return f.err
	}
	nextToken := f.listSnapshotsResponse.GetNextToken()
	if nextToken != nextTokenString {
		return fmt.Errorf("Expected nextToken %s got %s", nextTokenString, nextToken)
	}
	entries := f.listSnapshotsResponse.GetEntries()
	expectedEntries, err := strconv.Atoi(listed)
	if err != nil {
		return err
	}
	if entries == nil || len(entries) != expectedEntries {
		return fmt.Errorf("Expected %d List SnapshotResponse entries but got %d", expectedEntries, len(entries))
	}
	for j := 0; j < expectedEntries; j++ {
		entry := entries[j]
		id := entry.GetSnapshot().SnapshotId
		if expectedEntries <= 10 {
			ts := ptypes.TimestampString(entry.GetSnapshot().CreationTime)
			fmt.Printf("snapshot ID %s source ID %s timestamp %s\n", id, entry.GetSnapshot().SourceVolumeId, ts)
		}
		if f.listedVolumeIDs[id] {
			return fmt.Errorf("Got duplicate snapshot ID: " + id)
		}
		f.listedVolumeIDs[id] = true
	}
	fmt.Printf("Total snapshots received: %d\n", len(f.listedVolumeIDs))
	return nil
}

func (f *feature) theTotalSnapshotsListedIs(arg1 string) error {
	expectedSnapshots, err := strconv.Atoi(arg1)
	if err != nil {
		return err
	}
	if len(f.listedVolumeIDs) != expectedSnapshots {
		return fmt.Errorf("expected %d snapshots to be listed but got %d", expectedSnapshots, len(f.listedVolumeIDs))
	}
	return nil
}

func (f *feature) iInvalidateTheProbeCache() error {
	f.service.adminClient = nil
	f.service.system = nil
	return nil
}

func (f *feature) iInvalidateTheNodeID() error {
	f.service.opts.NodeName = ""
	return nil
}

func (f *feature) iQueueForDeletion(volumeName string) error {
	// First, we have to find the volumeID from the volumeName.
	volumeID := f.volumeNameToID[volumeName]
	if volumeID == "" {
		return fmt.Errorf("Could not find volumeID for volume %s", volumeName)
	}
	_, arrayID, devID, _ := f.service.parseCSIVolumeID(volumeID)
	var volumeSize float64
	if vol, ok := mock.Data.VolumeIDToVolume[devID]; ok {
		volumeSize = vol.CapacityGB
	} else {
		return fmt.Errorf("Could not find devID for volume %s", volumeName)
	}
	//volumeSize := mock.Data.VolumeIDToVolume[devID].CapacityGB
	req := &deletionWorkerRequest{
		symmetrixID:           arrayID,
		volumeID:              devID,
		volumeName:            "csi-" + f.service.opts.ClusterPrefix + "-" + volumeName,
		volumeSizeInCylinders: int64(volumeSize),
	}
	delWorker.requestDeletion(req)
	return nil
}

func (f *feature) deletionWorkerProcessesWhichResultsIn(volumeName, errormsg string) error {
	volumeName = "csi-" + f.service.opts.ClusterPrefix + "-" + volumeName
	// wait until the job completes
	for i := 1; i < 20; i++ {
		if delWorker == nil {
			return fmt.Errorf("delWorker nil")
		}
		// Look for the volumeName in the CompletedRequests
		for _, req := range delWorker.CompletedRequests {
			fmt.Printf("CompletedRequest: %#v\n", req)
			if req.volumeName == volumeName {
				// Found the volume
				if errormsg == "none" {
					if req.err == nil {
						return nil
					}
					return fmt.Errorf("Expected no error but got: %s", req.err.Error())
				}
				// We expected an error
				if req.err == nil {
					return fmt.Errorf("Expected error %s but got none", errormsg)
				}
				if !strings.Contains(req.err.Error(), errormsg) {
					return fmt.Errorf("Expected error to contain %s: but got: %s", errormsg, req.err.Error())
				}
				return nil
			}
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("timed out looking for CompletedRequest for volume: %s", volumeName)
}

func (f *feature) existingVolumesToBeDeleted(nvols int) error {
	mock.AddStorageGroup(defaultStorageGroup, "SRP_1", "Diamond")
	for i := 0; i < nvols; i++ {
		id := fmt.Sprintf("0000%d", i)
		mock.AddOneVolumeToStorageGroup(id, volDeleteKey+"-"+f.service.getClusterPrefix()+id, defaultStorageGroup, 8)
		resourceLink := fmt.Sprintf("sloprovisioning/system/%s/volume/%s", mock.DefaultSymmetrixID, id)
		job := mock.NewMockJob("job"+id, types.JobStatusRunning, types.JobStatusRunning, resourceLink)
		job.Job.Status = types.JobStatusRunning
	}
	return nil
}

func (f *feature) iRestartTheDeletionWorker() error {
	f.err = f.service.startDeletionWorker()
	return nil
}

func (f *feature) volumesAreBeingProcessedForDeletion(nVols int) error {
	if f.err != nil {
		return nil
	}
	// Count the number of volumes in the delWorker queue
	cnt := 0
	for i := 0; i < len(delWorker.Queue); i++ {
		if delWorker.Queue[i].symmetrixID == mock.DefaultSymmetrixID {
			cnt++
		}
	}
	if cnt < (nVols-2) || cnt > nVols {
		return fmt.Errorf("Expected at least %d volumes and not more than %d volumes in deletion queue but got %d", nVols-2, nVols, cnt)
	}
	return nil
}

func (f *feature) iRequestAPortGroup() error {
	f.selectedPortGroup, f.err = f.service.SelectPortGroup()
	if f.err != nil {
		return fmt.Errorf("Error selecting a Port Group from list of (%s): %v", f.service.opts.PortGroups, f.err)
	}
	if inducedErrors.portGroupError {
		f.service.opts.PortGroups = make([]string, 0)
	}
	return nil
}

func (f *feature) aValidPortGroupIsReturned() error {
	if f.selectedPortGroup == "" {
		return fmt.Errorf("Error selecting a Port Group: %v", f.err)
	}
	return nil
}

func (f *feature) iInvokeCreateOrUpdateHost(hostName string) error {
	f.service.SetPmaxTimeoutSeconds(3)
	symID := mock.DefaultSymmetrixID
	if inducedErrors.noSymID {
		symID = ""
	}
	fmt.Println("Hostname: " + hostName)
	fmt.Println("f.hostID: " + f.hostID)
	hostID := hostName
	if hostName == "" {
		hostID = f.hostID
	}
	if inducedErrors.noNodeName {
		hostID = ""
	}
	fmt.Println("hostID: " + hostID)
	initiators := []string{defaultInitiator}
	if inducedErrors.noIQNs {
		initiators = initiators[:0]
	}
	f.host, f.err = f.service.createOrUpdateHost(symID, hostID, initiators)
	f.IQNs = f.host.Initiators
	return nil
}

func (f *feature) initiatorsAreFound(expected int) error {
	if expected != len(f.IQNs) {
		return fmt.Errorf("Expected %d initiators but found %d", expected, len(f.IQNs))
	}
	return nil
}

func (f *feature) iInvokeNodeHostSetupWithAService(mode string) error {
	initiators := []string{defaultInitiator}
	f.service.mode = mode
	f.service.SetPmaxTimeoutSeconds(30)
	f.err = f.service.nodeHostSetup(initiators, true)
	return nil
}

func (f *feature) theErrorClearsAfterSeconds(seconds int64) error {
	go func(seconds int64) {
		time.Sleep(time.Duration(seconds) * time.Second)
		mock.InducedErrors.GetSymmetrixError = false
	}(seconds)
	return nil
}

func (f *feature) aProvidedArrayWhitelistOf(whitelist string) error {
	f.err = f.service.setArrayWhitelist(whitelist)
	return nil
}

func (f *feature) iInvokeGetArrayWhitelist() error {
	f.allowedArrays = f.service.getArrayWhitelist()
	return nil
}

func (f *feature) arraysAreFound(count int) error {
	if len(f.allowedArrays) != count {
		return fmt.Errorf("Expected %d arrays in the whitelist but found %d", count, len(f.allowedArrays))
	}
	return nil
}

type GetVolumeByIDResponse struct {
	sym string
	dev string
	vol *types.Volume
	err error
}

func (f *feature) iCallGetVolumeByID() error {
	var id string
	if !inducedErrors.noVolumeID {
		if inducedErrors.invalidVolumeID {
			id = f.service.createCSIVolumeID(f.service.getClusterPrefix(), goodVolumeName, mock.DefaultSymmetrixID, "99999")
		} else if inducedErrors.differentVolumeID {
			id = f.service.createCSIVolumeID(f.service.getClusterPrefix(), altVolumeName, mock.DefaultSymmetrixID, goodVolumeID)
		} else {
			id = f.service.createCSIVolumeID(f.service.getClusterPrefix(), goodVolumeName, mock.DefaultSymmetrixID, goodVolumeID)
		}
	}
	sym, dev, vol, err := f.service.GetVolumeByID(id)
	resp := &GetVolumeByIDResponse{
		sym: sym,
		dev: dev,
		vol: vol,
		err: err,
	}
	f.getVolumeByIDResponse = resp
	f.err = err
	return nil
}

func (f *feature) aValidGetVolumeByIDResultIsReturnedIfNoError() error {
	if f.err != nil {
		return nil
	}
	if f.getVolumeByIDResponse == nil {
		return errors.New("Expected a GetVolumeByIDResult")
	}
	if f.getVolumeByIDResponse.sym != mock.DefaultSymmetrixID {
		return fmt.Errorf("Expected sym %s but got %s", mock.DefaultSymmetrixID, f.getVolumeByIDResponse.sym)
	}
	if f.getVolumeByIDResponse.dev != goodVolumeID {
		return fmt.Errorf("Expected dev %s but got %s", goodVolumeID, f.getVolumeByIDResponse.dev)
	}
	return nil
}

func (f *feature) iCallNodeGetVolumeStats() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodeGetVolumeStatsRequest)
	_, f.err = f.service.NodeGetVolumeStats(ctx, req)
	return nil
}

func (f *feature) iCallCreateSnapshot() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.CreateSnapshotRequest)
	_, f.err = f.service.CreateSnapshot(ctx, req)
	return nil
}

func (f *feature) iCallListVolumes() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.ListVolumesRequest)
	_, f.err = f.service.ListVolumes(ctx, req)
	return nil
}

func (f *feature) iCallListSnapshots() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.ListSnapshotsRequest)
	_, f.err = f.service.ListSnapshots(ctx, req)
	return nil
}

func (f *feature) iHaveAVolumeWithInvalidVolumeIdentifier() error {
	devID := goodVolumeID
	volumeIdentifier := csiPrefix + f.service.getClusterPrefix() + "-" + "xyz"
	sgList := make([]string, 1)
	sgList[0] = defaultStorageGroup
	mock.AddStorageGroup(defaultStorageGroup, "SRP_1", "Optimized")
	mock.AddOneVolumeToStorageGroup(devID, volumeIdentifier, defaultStorageGroup, 1)
	f.volumeID = f.service.createCSIVolumeID(f.service.getClusterPrefix(), goodVolumeName, mock.DefaultSymmetrixID, goodVolumeID)
	return nil
}

func (f *feature) thereAreNoArraysLoggedIn() error {
	f.service.loggedInArrays = make(map[string]bool)
	return nil
}

func (f *feature) iInvokeEnsureLoggedIntoEveryArray() error {
	f.service.SetPmaxTimeoutSeconds(3)
	f.err = f.service.ensureLoggedIntoEveryArray(false)
	return nil
}

func (f *feature) arraysAreLoggedIn(count int) error {
	if count != len(f.service.loggedInArrays) {
		return fmt.Errorf("Expected %d arrays logged in but go %d", count, len(f.service.loggedInArrays))
	}
	return nil
}

func (f *feature) iCallGetTargetsForMaskingView() error {
	// First we have to read the masking view
	fmt.Printf("f.mvID %s\n", f.mvID)
	symID := mock.DefaultSymmetrixID
	if inducedErrors.noSymID {
		symID = ""
	}
	var view *types.MaskingView
	view, f.err = f.service.adminClient.GetMaskingViewByID(mock.DefaultSymmetrixID, f.mvID)
	if view == nil {
		f.err = fmt.Errorf("view is nil")
	}
	f.iscsiTargets, f.err = f.service.getTargetsForMaskingView(symID, view)
	return nil
}

func (f *feature) theResultHasPorts(expected string) error {
	if f.err != nil {
		return nil
	}
	portsExpected, _ := strconv.Atoi(expected)
	if len(f.iscsiTargets) != portsExpected {
		return fmt.Errorf("Expected %d ports but got %d ports", portsExpected, len(f.iscsiTargets))
	}
	return nil
}

func FeatureContext(s *godog.Suite) {
	f := &feature{}
	s.Step(`^a PowerMax service$`, f.aPowerMaxService)
	s.Step(`^I call GetPluginInfo$`, f.iCallGetPluginInfo)
	s.Step(`^a valid GetPluginInfoResponse is returned$`, f.aValidGetPluginInfoResponseIsReturned)
	s.Step(`^I call GetPluginCapabilities$`, f.iCallGetPluginCapabilities)
	s.Step(`^a valid GetPluginCapabilitiesResponse is returned$`, f.aValidGetPluginCapabilitiesResponseIsReturned)
	s.Step(`^I call Probe$`, f.iCallProbe)
	s.Step(`^a valid ProbeResponse is returned$`, f.aValidProbeResponseIsReturned)
	s.Step(`^the error contains "([^"]*)"$`, f.theErrorContains)
	s.Step(`^the possible error contains "([^"]*)"$`, f.thePossibleErrorContains)
	s.Step(`^the Controller has no connection$`, f.theControllerHasNoConnection)
	s.Step(`^there is a Node Probe Lsmod error$`, f.thereIsANodeProbeLsmodError)
	s.Step(`^I call CreateVolume "([^"]*)"$`, f.iCallCreateVolume)
	s.Step(`^a valid CreateVolumeResponse is returned$`, f.aValidCreateVolumeResponseIsReturned)
	s.Step(`^I specify AccessibilityRequirements$`, f.iSpecifyAccessibilityRequirements)
	s.Step(`^I specify MULTINODEWRITER$`, f.iSpecifyMULTINODEWRITER)
	s.Step(`^I specify a BadCapacity$`, f.iSpecifyABadCapacity)
	s.Step(`^I specify a ApplicationPrefix$`, f.iSpecifyAApplicationPrefix)
	s.Step(`^I specify a StorageGroup$`, f.iSpecifyAStorageGroup)
	s.Step(`^I specify NoStoragePool$`, f.iSpecifyNoStoragePool)
	s.Step(`^I call CreateVolumeSize "([^"]*)" "(\d+)"$`, f.iCallCreateVolumeSize)
	s.Step(`^I change the StoragePool "([^"]*)"$`, f.iChangeTheStoragePool)
	s.Step(`^I induce error "([^"]*)"$`, f.iInduceError)
	s.Step(`^I specify VolumeContentSource$`, f.iSpecifyVolumeContentSource)
	s.Step(`^I specify CreateVolumeMountRequest "([^"]*)"$`, f.iSpecifyCreateVolumeMountRequest)
	s.Step(`^I call PublishVolume with "([^"]*)" to "([^"]*)"$`, f.iCallPublishVolumeWithTo)
	s.Step(`^a valid PublishVolumeResponse is returned$`, f.aValidPublishVolumeResponseIsReturned)
	s.Step(`^a valid volume$`, f.aValidVolume)
	s.Step(`^an invalid volume$`, f.anInvalidVolume)
	s.Step(`^no volume$`, f.noVolume)
	s.Step(`^no node$`, f.noNode)
	s.Step(`^no volume capability$`, f.noVolumeCapability)
	s.Step(`^no access mode$`, f.noAccessMode)
	s.Step(`^then I use a different nodeID$`, f.thenIUseADifferentNodeID)
	s.Step(`^I use AccessType Mount$`, f.iUseAccessTypeMount)
	s.Step(`^no error was received$`, f.noErrorWasReceived)
	s.Step(`^I call UnpublishVolume from "([^"]*)"$`, f.iCallUnpublishVolumeFrom)
	s.Step(`^a valid UnpublishVolumeResponse is returned$`, f.aValidUnpublishVolumeResponseIsReturned)
	s.Step(`^I call NodeGetInfo$`, f.iCallNodeGetInfo)
	s.Step(`^a valid NodeGetInfoResponse is returned$`, f.aValidNodeGetInfoResponseIsReturned)
	s.Step(`^I call DeleteVolume with "([^"]*)"$`, f.iCallDeleteVolumeWith)
	s.Step(`^a valid DeleteVolumeResponse is returned$`, f.aValidDeleteVolumeResponseIsReturned)
	s.Step(`^I call GetCapacity with storage pool "([^"]*)"$`, f.iCallGetCapacityWithStoragePool)
	s.Step(`^I call GetCapacity without Symmetrix ID$`, f.iCallGetCapacityWithoutSymmetrixID)
	s.Step(`^I call GetCapacity without Parameters$`, f.iCallGetCapacityWithoutParameters)
	s.Step(`^I call GetCapacity with Invalid capabilities$`, f.iCallGetCapacityWithInvalidCapabilities)
	s.Step(`^a valid GetCapacityResponse is returned$`, f.aValidGetCapacityResponseIsReturned)
	s.Step(`^I call ControllerGetCapabilities$`, f.iCallControllerGetCapabilities)
	s.Step(`^a valid ControllerGetCapabilitiesResponse is returned$`, f.aValidControllerGetCapabilitiesResponseIsReturned)
	s.Step(`^I call ValidateVolumeCapabilities with voltype "([^"]*)" access "([^"]*)" fstype "([^"]*)" pool "([^"]*)" level "([^"]*)"$`, f.iCallValidateVolumeCapabilitiesWithVoltypeAccessFstype)
	s.Step(`^a valid ListVolumesResponse is returned$`, f.aValidListVolumesResponseIsReturned)
	s.Step(`^I call(?:ed)? ListVolumes with$`, f.iCallListVolumesWith)
	s.Step(`^I call(?:ed)? ListVolumes again with$`, f.iCallListVolumesAgainWith)
	s.Step(`^I call ListVolumes$`, f.iCallListVolumes)
	s.Step(`^there (?:are|is) (\d+) valid volumes?$`, f.thereAreValidVolumes)
	s.Step(`^(\d+) volume(?:s)? (?:are|is) listed$`, f.volumesAreListed)
	s.Step(`^an invalid ListVolumesResponse is returned$`, f.anInvalidListVolumesResponseIsReturned)
	s.Step(`^a capability with voltype "([^"]*)" access "([^"]*)" fstype "([^"]*)"$`, f.aCapabilityWithVoltypeAccessFstype)
	s.Step(`^a controller published volume$`, f.aControllerPublishedVolume)
	s.Step(`^I call NodePublishVolume$`, f.iCallNodePublishVolume)
	s.Step(`^get Node Publish Volume Request$`, f.getNodePublishVolumeRequest)
	s.Step(`^I mark request read only$`, f.iMarkRequestReadOnly)
	s.Step(`^I call NodeUnpublishVolume$`, f.iCallNodeUnpublishVolume)
	s.Step(`^there are no remaining mounts$`, f.thereAreNoRemainingMounts)
	s.Step(`^I call BeforeServe$`, f.iCallBeforeServe)
	s.Step(`^I call BeforeServe without ClusterPrefix$`, f.iCallBeforeServeWithoutClusterPrefix)
	s.Step(`^I call BeforeServe with an invalid ClusterPrefix$`, f.iCallBeforeServeWithAnInvalidClusterPrefix)
	s.Step(`^I call NodeStageVolume$`, f.iCallNodeStageVolume)
	s.Step(`^I call NodeUnstageVolume$`, f.iCallNodeUnstageVolume)
	s.Step(`^I call NodeGetCapabilities$`, f.iCallNodeGetCapabilities)
	s.Step(`^a valid NodeGetCapabilitiesResponse is returned$`, f.aValidNodeGetCapabilitiesResponseIsReturned)
	s.Step(`^I call CreateSnapshot$`, f.iCallCreateSnapshot)
	s.Step(`^I call CreateSnapshot "([^"]*)"$`, f.iCallCreateSnapshotWith)
	s.Step(`^a valid CreateSnapshotResponse is returned$`, f.aValidCreateSnapshotResponseIsReturned)
	s.Step(`^a valid snapshot$`, f.aValidSnapshot)
	s.Step(`^I call DeleteSnapshot$`, f.iCallDeleteSnapshot)
	s.Step(`^a valid snapshot consistency group$`, f.aValidSnapshotConsistencyGroup)
	s.Step(`^I call Create Volume from Snapshot$`, f.iCallCreateVolumeFromSnapshot)
	s.Step(`^the wrong capacity$`, f.theWrongCapacity)
	s.Step(`^the wrong storage pool$`, f.theWrongStoragePool)
	s.Step(`^there are (\d+) valid snapshots of "([^"]*)" volume$`, f.thereAreValidSnapshotsOfVolume)
	s.Step(`^I call ListSnapshots$`, f.iCallListSnapshots)
	s.Step(`^I call ListSnapshots with max_entries "([^"]*)" and starting_token "([^"]*)"$`, f.iCallListSnapshotsWithMaxEntriesAndStartingToken)
	s.Step(`^a valid ListSnapshotsResponse is returned with listed "([^"]*)" and next_token "([^"]*)"$`, f.aValidListSnapshotsResponseIsReturnedWithListedAndNextToken)
	s.Step(`^the total snapshots listed is "([^"]*)"$`, f.theTotalSnapshotsListedIs)
	s.Step(`^I call ListSnapshots for volume "([^"]*)"$`, f.iCallListSnapshotsForVolume)
	s.Step(`^I call ListSnapshots for snapshot "([^"]*)"$`, f.iCallListSnapshotsForSnapshot)
	s.Step(`^the snapshot ID is "([^"]*)"$`, f.theSnapshotIDIs)
	s.Step(`^I invalidate the Probe cache$`, f.iInvalidateTheProbeCache)
	s.Step(`^I invalidate the NodeID$`, f.iInvalidateTheNodeID)
	s.Step(`^I queue "([^"]*)" for deletion$`, f.iQueueForDeletion)
	s.Step(`^deletion worker processes "([^"]*)" which results in "([^"]*)"$`, f.deletionWorkerProcessesWhichResultsIn)
	s.Step(`^I request a PortGroup$`, f.iRequestAPortGroup)
	s.Step(`^a valid PortGroup is returned$`, f.aValidPortGroupIsReturned)
	s.Step(`^I invoke createOrUpdateHost "([^"]*)"$`, f.iInvokeCreateOrUpdateHost)
	s.Step(`^I invoke nodeHostSetup with a "([^"]*)" service$`, f.iInvokeNodeHostSetupWithAService)
	s.Step(`^the error clears after (\d+) seconds$`, f.theErrorClearsAfterSeconds)
	s.Step(`^I have a Node "([^"]*)" with MaskingView$`, f.iHaveANodeWithMaskingView)
	s.Step(`^I have a Node "([^"]*)" with Host$`, f.iHaveANodeWithHost)
	s.Step(`^I have a Node "([^"]*)" with StorageGroup$`, f.iHaveANodeWithStorageGroup)
	s.Step(`^I have a Node "([^"]*)" with a FastManagedMaskingView$`, f.iHaveANodeWithAFastManagedMaskingView)
	s.Step(`^I have a Node "([^"]*)" with FastManagedStorageGroup$`, f.iHaveANodeWithFastManagedStorageGroup)
	s.Step(`^I add the Volume to "([^"]*)"$`, f.iAddTheVolumeTo)
	s.Step(`^(\d+) existing volumes to be deleted$`, f.existingVolumesToBeDeleted)
	s.Step(`^I restart the deletionWorker$`, f.iRestartTheDeletionWorker)
	s.Step(`^(\d+) volumes are being processed for deletion$`, f.volumesAreBeingProcessedForDeletion)
	s.Step(`^I change the target path$`, f.iChangeTheTargetPath)
	s.Step(`^a provided array whitelist of "([^"]*)"$`, f.aProvidedArrayWhitelistOf)
	s.Step(`^I invoke getArrayWhitelist$`, f.iInvokeGetArrayWhitelist)
	s.Step(`^(\d+) arrays are found$`, f.arraysAreFound)
	s.Step(`^I call GetVolumeByID$`, f.iCallGetVolumeByID)
	s.Step(`^a valid GetVolumeByID result is returned if no error$`, f.aValidGetVolumeByIDResultIsReturnedIfNoError)
	s.Step(`^(\d+) initiators are found$`, f.initiatorsAreFound)
	s.Step(`^I call NodeGetVolumeStats$`, f.iCallNodeGetVolumeStats)
	s.Step(`^I have a volume with invalid volume identifier$`, f.iHaveAVolumeWithInvalidVolumeIdentifier)
	s.Step(`^there are no arrays logged in$`, f.thereAreNoArraysLoggedIn)
	s.Step(`^I invoke ensureLoggedIntoEveryArray$`, f.iInvokeEnsureLoggedIntoEveryArray)
	s.Step(`^(\d+) arrays are logged in$`, f.arraysAreLoggedIn)
	s.Step(`^I call GetTargetsForMaskingView$`, f.iCallGetTargetsForMaskingView)
	s.Step(`^the result has "([^"]*)" ports$`, f.theResultHasPorts)
}