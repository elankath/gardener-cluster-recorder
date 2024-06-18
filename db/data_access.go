package db

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/blockloop/scan/v2"
	gcr "github.com/elankath/gardener-cluster-recorder"
	_ "github.com/glebarez/go-sqlite"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"log/slog"
	"strings"
	"time"
)

type DataAccess struct {
	io.Closer
	dataDBPath                                  string
	dataDB                                      *sql.DB
	insertWorkerPoolInfo                        *sql.Stmt
	selectWorkerPoolInfosBefore                 *sql.Stmt
	insertMCDInfo                               *sql.Stmt
	updateMCDInfoDeletionTimeStamp              *sql.Stmt
	selectMCDInfoHash                           *sql.Stmt
	selectMCDInfoBefore                         *sql.Stmt
	selectLatestMCDInfo                         *sql.Stmt
	insertEvent                                 *sql.Stmt
	insertNodeInfo                              *sql.Stmt
	updateNodeInfoDeletionTimeStamp             *sql.Stmt
	insertPodInfo                               *sql.Stmt
	insertPDB                                   *sql.Stmt
	updatePodDeletionTimeStamp                  *sql.Stmt
	updatePdbDeletionTimeStamp                  *sql.Stmt
	selectLatestPodInfoWithName                 *sql.Stmt
	selectPodCountWithUIDAndHash                *sql.Stmt
	selectEventWithUID                          *sql.Stmt
	selectAllEvents                             *sql.Stmt
	selectUnscheduledPodsBeforeTimestamp        *sql.Stmt
	selectScheduledPodsBeforeSnapshotTimestamp  *sql.Stmt
	selectLatestPodInfosBeforeSnapshotTimestamp *sql.Stmt
	selectAllActiveNodeGroupsDesc               *sql.Stmt
	selectNodeInfosBefore                       *sql.Stmt
	selectNodeCountWithNameAndHash              *sql.Stmt
	selectLatestCADeployment                    *sql.Stmt
	insertCADeployment                          *sql.Stmt
	selectCADeploymentByHash                    *sql.Stmt
	selectLatestNodesBeforeAndNotDeleted        *sql.Stmt
}

func NewDataAccess(dataDBPath string) *DataAccess {
	access := &DataAccess{
		dataDBPath: dataDBPath,
	}
	return access
}

func (d *DataAccess) Init() error {
	db, err := sql.Open("sqlite", d.dataDBPath)
	if err != nil {
		return fmt.Errorf("cannot open db: %w", err)
	}
	d.dataDB = db
	err = d.createSchema()
	if err != nil {
		return fmt.Errorf("error creating db schema: %w", err)
	}
	err = d.prepareStatements()
	if err != nil {
		return fmt.Errorf("error preparing statements: %w", err)
	}
	return nil
}

func (d *DataAccess) Close() error {
	if d.dataDB == nil {
		return nil
	}
	slog.Info("stopping data db", "dataDBPath", d.dataDBPath)
	err := d.dataDB.Close()
	if err != nil {
		slog.Warn("cannot close data db", "error", err)
		return err
	}
	d.dataDB = nil
	return nil
}

func (d *DataAccess) prepareStatements() (err error) {
	db := d.dataDB
	d.insertWorkerPoolInfo, err = db.Prepare(InsertWorkerPoolInfo)
	if err != nil {
		return fmt.Errorf("cannot prepare insertWorkerPoolInfo statement: %w", err)
	}

	d.selectWorkerPoolInfosBefore, err = db.Prepare(SelectWorkerPoolInfoBefore)
	if err != nil {
		return fmt.Errorf("cannot prepare selectWorkerPoolInfosBefore statement: %w", err)
	}

	d.insertNodeInfo, err = db.Prepare(InsertNodeInfo)
	if err != nil {
		return fmt.Errorf("cannot prepare insertNodeInfo statement: %w", err)
	}
	d.updateNodeInfoDeletionTimeStamp, err = db.Prepare(UpdateNodeInfoDeletionTimestamp)
	if err != nil {
		return fmt.Errorf("cannot prepare updateNodeInfoDeletionTimeStamp: %w", err)
	}

	d.selectNodeInfosBefore, err = db.Prepare(SelectNodeInfoBefore)
	if err != nil {
		return fmt.Errorf("cannot prepare selectNodeInfosBefore statement: %w", err)
	}

	pdbInsertStmt, err := db.Prepare("INSERT INTO pdb_info(uid,name,generation,creationTimestamp,minAvailable,maxUnAvailable,spec) VALUES(?,?,?,?,?,?,?)")
	if err != nil {
		return fmt.Errorf("cannot prepare pdb insert statement : %w", err)
	}
	d.insertPDB = pdbInsertStmt

	d.updatePdbDeletionTimeStamp, err = db.Prepare("UPDATE pdb_info SET DeletionTimeStamp=? WHERE uid=?")
	if err != nil {
		return fmt.Errorf("cannot prepare updatePdbDeletionTimeStamp: %w", err)
	}

	d.updateMCDInfoDeletionTimeStamp, err = db.Prepare(UpdateMCDInfoDeletionTimestamp)
	if err != nil {
		return fmt.Errorf("cannot prepare updateMCDInfoDeletionTimeStamp: %w", err)
	}

	d.selectMCDInfoBefore, err = db.Prepare(SelectMCDBefore)
	if err != nil {
		return fmt.Errorf("cannot prepare selectNodeGroupsBefore statement: %w", err)
	}

	d.selectAllActiveNodeGroupsDesc, err = db.Prepare("SELECT * from nodegroup_info where  DeletionTimestamp is null  order by RowID desc")
	if err != nil {
		return fmt.Errorf("cannot prepare get active node groups  statement: %w", err)
	}
	d.selectMCDInfoHash, err = db.Prepare(SelectMCDInfoHash)
	if err != nil {
		return fmt.Errorf("cannot prepare selectMCDInfoHash: %w", err)
	}

	d.selectLatestMCDInfo, err = db.Prepare(SelectLatestMCDInfo)
	if err != nil {
		return fmt.Errorf("cannot prepare selectLatestMCDInfo: %w", err)
	}

	d.selectLatestPodInfoWithName, err = db.Prepare("SELECT * FROM pod_info WHERE Name=? ORDER BY CreationTimestamp DESC LIMIT 1")
	if err != nil {
		return fmt.Errorf("cannot prepare selectLatestPodInfoWithName: %w", err)
	}
	d.insertEvent, err = db.Prepare(InsertEvent)
	if err != nil {
		return fmt.Errorf("cannot prepare events insert statement: %w", err)
	}
	d.insertPodInfo, err = db.Prepare(InsertPodInfo)
	if err != nil {
		return fmt.Errorf("cannot prepare pod insert statement: %w", err)
	}

	//TODO: must create indexes
	d.selectPodCountWithUIDAndHash, err = db.Prepare(SelectPodCountWithUIDAndHash)
	if err != nil {
		return fmt.Errorf("cannot prepare selectPodCountWithUIDAndHash: %w", err)
	}

	d.updatePodDeletionTimeStamp, err = db.Prepare(UpdatePodDeletionTimestamp)
	if err != nil {
		return fmt.Errorf("cannot prepare updatePodDeletionTimeStamp: %w", err)
	}

	d.insertMCDInfo, err = db.Prepare(InsertMCDInfo)
	if err != nil {
		return fmt.Errorf("cannot prepare insertMCDInfo: %w", err)
	}

	d.selectEventWithUID, err = db.Prepare("SELECT * from event_info where UID = ?")

	d.selectAllEvents, err = db.Prepare("SELECT * from event_info ORDER BY EventTime")
	if err != nil {
		return fmt.Errorf("cannot prepare selectAllEvents statement: %w", err)
	}

	d.selectUnscheduledPodsBeforeTimestamp, err = db.Prepare(SelectPodsWithEmptyNameAndBeforeCreationTimestamp)
	if err != nil {
		return fmt.Errorf("cannot prepare selectUnscheduledPodsBeforeTimestamp statement: %w", err)
	}

	d.selectScheduledPodsBeforeSnapshotTimestamp, err = db.Prepare(SelectLatestScheduledPodsBeforeSnapshotTimestamp)
	if err != nil {
		return fmt.Errorf("cannot prepare selectScheduledPodsBeforeSnapshotTimestamp statement: %w", err)
	}

	d.selectLatestPodInfosBeforeSnapshotTimestamp, err = db.Prepare(SelectLatestPodsBeforeSnapshotTimestamp)
	if err != nil {
		return fmt.Errorf("cannot prepare selectLatestPodInfosBeforeSnapshotTimestamp statement: %w", err)
	}

	d.selectNodeCountWithNameAndHash, err = db.Prepare(SelectNodeCountWithNameAndHash)
	if err != nil {
		return fmt.Errorf("cannot prepare selectNodeCountWithNameAndHash: %w", err)
	}

	d.selectCADeploymentByHash, err = db.Prepare(SelectCADeploymentByHash)
	if err != nil {
		return fmt.Errorf("cannot prepare selectCADeploymentByHash: %w", err)
	}

	d.selectLatestCADeployment, err = db.Prepare(SelectLatestCADeployment)
	if err != nil {
		return fmt.Errorf("cannot prepare selectLatestCADeployment")
	}

	d.insertCADeployment, err = db.Prepare(InsertCADeployment)
	if err != nil {
		return fmt.Errorf("cannot prepare insertCADeployment statement")
	}

	d.selectLatestNodesBeforeAndNotDeleted, err = db.Prepare(SelectLatestNodesBeforeAndNotDeleted)
	if err != nil {
		return fmt.Errorf("cannot prepare ")
	}

	return err
}
func (d *DataAccess) createSchema() error {
	var db = d.dataDB
	var err error
	var result sql.Result

	result, err = db.Exec(CreateWorkerPoolInfo)
	if err != nil {
		return fmt.Errorf("cannot create worker_pool_info table: %w", err)
	}
	slog.Info("successfully created worker_pool_info table", "result", result)

	result, err = db.Exec(CreateMCDInfoTable)
	if err != nil {
		return fmt.Errorf("cannot create mcd_info table: %w", err)
	}
	slog.Info("successfully created mcd_info table", "result", result)

	result, err = db.Exec(CreateEventInfoTable)
	if err != nil {
		return fmt.Errorf("cannot create event_info table: %w", err)
	}

	slog.Info("successfully created event_info table", "result", result)

	//result, err = db.Exec(CreateNodeGroupInfoTable)
	//if err != nil {
	//	return fmt.Errorf("cannot create nodegroup_info table: %w", err)
	//}
	//slog.Info("successfully created nodegroup_info table", "result", result)

	result, err = db.Exec(CreateNodeInfoTable)
	if err != nil {
		return fmt.Errorf("cannot create node_info table : %w", err)
	}
	slog.Info("successfully created node_info table", "result", result)

	result, err = db.Exec(CreatePodInfoTable)
	if err != nil {
		return fmt.Errorf("cannot create pod_info table: %w", err)
	}
	slog.Info("successfully created pod_info table", "result", result)

	result, err = db.Exec(`CREATE TABLE IF NOT EXISTS pdb_info(
    							id INTEGER PRIMARY KEY AUTOINCREMENT,
    							uid TEXT,
    							name TEXT,
    							generation INT,
    							creationTimestamp DATETIME,
    							deletionTimestamp DATETIME,
    							minAvailable TEXT,
    							maxUnAvailable TEXT,
    							spec TEXT)`) // TODO: maxUnAvailable -> maxUnavailable
	if err != nil {
		return fmt.Errorf("cannot create pdb_info table: %w", err)
	}
	slog.Info("successfully created pdb_info table", "result", result)

	result, err = db.Exec(CreateCASettingsInfoTable)
	if err != nil {
		return fmt.Errorf("cannot create ca_settings_info table: %w", err)
	}
	slog.Info("successfully created the ca_settings_info table")

	return nil
}

func (d *DataAccess) CountPodInfoWithSpecHash(uid, hash string) (int, error) {
	var count sql.NullInt32
	err := d.selectPodCountWithUIDAndHash.QueryRow(uid, hash).Scan(&count)
	if count.Valid {
		return int(count.Int32), nil
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, nil
		}
	}
	return -1, err
}

func (d *DataAccess) CountNodeInfoWithHash(name, hash string) (int, error) {
	var count sql.NullInt32
	err := d.selectNodeCountWithNameAndHash.QueryRow(name, hash).Scan(&count)
	if count.Valid {
		return int(count.Int32), nil
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, nil
		}
	}
	return -1, err
}

func (d *DataAccess) UpdatePodDeletionTimestamp(podUID types.UID, deletionTimestamp time.Time) (updated int64, err error) {
	result, err := d.updatePodDeletionTimeStamp.Exec(deletionTimestamp.UTC().UnixMilli(), podUID)
	if err != nil {
		return -1, err
	}
	updated, err = result.RowsAffected()
	if err != nil {
		return -1, err
	}
	return updated, err
}

func (d *DataAccess) UpdateNodeInfoDeletionTimestamp(name string, deletionTimestamp time.Time) (updated int64, err error) {
	result, err := d.updateNodeInfoDeletionTimeStamp.Exec(deletionTimestamp, name)
	if err != nil {
		return -1, err
	}
	updated, err = result.RowsAffected()
	if err != nil {
		return -1, err
	}
	return updated, err
}

func (d *DataAccess) UpdateMCDInfoDeletionTimestamp(name string, deletionTimestamp time.Time) (updated int64, err error) {
	result, err := d.updateMCDInfoDeletionTimeStamp.Exec(deletionTimestamp, name)
	if err != nil {
		return -1, err
	}
	updated, err = result.RowsAffected()
	if err != nil {
		return -1, err
	}
	return updated, err
}

func (d *DataAccess) StoreEventInfo(event gcr.EventInfo) error {
	//eventsStmt, err := db.Prepare("INSERT INTO event_info(UID, EventTime, ReportingController, Reason, Message, InvolvedObjectKind, InvolvedObjectName, InvolvedObjectNamespace, InvolvedObjectUID) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)")
	_, err := d.insertEvent.Exec(
		event.UID,
		event.EventTime,
		event.ReportingController,
		event.Reason,
		event.Message,
		event.InvolvedObjectKind,
		event.InvolvedObjectName,
		event.InvolvedObjectNamespace,
		event.InvolvedObjectUID,
	)
	return err
}

func (d *DataAccess) GetMachineDeploymentInfoHash(name string) (string, error) {
	return getHash(d.selectMCDInfoHash, name)
}

func (d *DataAccess) StoreMachineDeploymentInfo(m gcr.MachineDeploymentInfo) (rowID int64, err error) {
	if m.Hash == "" {
		m.Hash = m.GetHash()
	}
	result, err := d.insertMCDInfo.Exec(
		m.CreationTimestamp.UTC().UnixMilli(),
		m.SnapshotTimestamp.UTC().UnixMilli(),
		m.Name,
		m.Namespace,
		m.Replicas,
		m.PoolName,
		m.Zone,
		m.MaxSurge.String(),
		m.MaxUnavailable.String(),
		m.MachineClassName,
		m.Hash)
	if err != nil {
		slog.Error("cannot insert MachineDeploymentInfo in the mcd_info table", "error", err)
		return
	}
	rowID, err = result.LastInsertId()
	if err != nil {
		slog.Error("cannot retrieve rowID for MachineDeploymentInfo from the mcd_info table", "error", err, "name", m.Name)
		return
	}
	slog.Info("StoreMachineDeploymentInfo successful.", "Name", m.Name,
		"RowID", rowID,
		"Replicas",
		m.Replicas,
		"Hash",
		m.Hash,
	)
	return
}

func (d *DataAccess) StoreWorkerPoolInfo(w gcr.WorkerPoolInfo) (rowID int64, err error) {
	if w.Hash == "" {
		w.Hash = w.GetHash()
	}
	result, err := d.insertWorkerPoolInfo.Exec(
		w.CreationTimestamp.UTC().UnixMilli(),
		w.SnapshotTimestamp.UTC().UnixMilli(),
		w.Name,
		w.Namespace,
		w.MachineType,
		w.Architecture,
		w.Minimum,
		w.Maximum,
		w.MaxSurge.String(),
		w.MaxUnavailable.String(),
		strings.Join(w.Zones, " "),
		w.Hash)
	if err != nil {
		slog.Error("cannot insert WorkerPoolInfo in the worker_pool_info table", "error", err, "workerPoolInfo", workerPoolRow{})
		return
	}
	rowID, err = result.LastInsertId()
	if err != nil {
		slog.Error("cannot retrieve rowID for WorkerPoolInfo from the worker_pool_info table", "error", err, "name", w.Name)
		return
	}
	slog.Info("StoreWorkerPoolInfo successful.",
		"RowID", rowID,
		"Name", w.Name,
		"Minimum", w.Minimum,
		"Maximum", w.Maximum,
		"Hash", w.Hash,
	)
	return
}

func (d *DataAccess) LoadWorkerPoolInfosBefore(snapshotTimestamp time.Time) ([]gcr.WorkerPoolInfo, error) {
	workerPoolInfos, err := queryAndMapToInfos[gcr.WorkerPoolInfo, workerPoolRow](d.selectWorkerPoolInfosBefore, snapshotTimestamp)
	if err != nil {
		return nil, fmt.Errorf("LoadWorkerPoolInfosBefore could not scan rows: %w", err)
	}
	return workerPoolInfos, nil
}

func (d *DataAccess) LoadMachineDeploymentInfosBefore(snapshotTimestamp time.Time) ([]gcr.MachineDeploymentInfo, error) {
	mcdInfos, err := queryAndMapToInfos[gcr.MachineDeploymentInfo, mcdRow](d.selectMCDInfoBefore, snapshotTimestamp)
	if err != nil {
		return nil, fmt.Errorf("LoadMachineDeploymentInfosBefore could not scan rows: %w", err)
	}
	return mcdInfos, nil
}

func (d *DataAccess) LoadLatestMachineDeploymentInfo(name string) (mcdInfo gcr.MachineDeploymentInfo, err error) {
	return queryAndMapToInfo[gcr.MachineDeploymentInfo, mcdRow](d.selectLatestMCDInfo, name)
}

func (d *DataAccess) LoadEventInfoWithUID(eventUID string) (eventInfo gcr.EventInfo, err error) {
	rows, err := d.selectEventWithUID.Query(eventUID)
	if err != nil { //TODO: wrap err with msg and return
		return
	}
	err = scan.Row(&eventInfo, rows) //TODO: wrap err with msg and return
	return
}

// LoadAllEvents TODO: move me to generics
func (d *DataAccess) LoadAllEvents() (events []gcr.EventInfo, err error) {
	rows, err := d.selectAllEvents.Query()
	if err != nil { //TODO: wrap err with msg and return
		return
	}
	err = scan.Rows(&events, rows)
	return
}

func (d *DataAccess) LoadLatestPodInfoWithName(podName string) (podInfo gcr.PodInfo, err error) {
	return queryAndMapToInfo[gcr.PodInfo, podRow](d.selectLatestPodInfoWithName, podName)
}

func (d *DataAccess) GetLatestUnscheduledPodsBeforeTimestamp(timeStamp time.Time) (podInfos []gcr.PodInfo, err error) {
	return queryAndMapToInfos[gcr.PodInfo, podRow](d.selectUnscheduledPodsBeforeTimestamp, timeStamp)
}

func (d *DataAccess) GetLatestPodInfosBeforeSnapshotTime(snapshotTime time.Time) (pods []gcr.PodInfo, err error) {
	return queryAndMapToInfos[gcr.PodInfo, podRow](d.selectLatestPodInfosBeforeSnapshotTimestamp, snapshotTime)
}

func (d *DataAccess) GetLatestScheduledPodsBeforeTimestamp(timestamp time.Time) (pods []gcr.PodInfo, err error) {
	slog.Info("GetLatestScheduledPodsBeforeTimestamp: selectScheduledPodsBeforeSnapshotTimestamp", "timestamp", timestamp.UTC().UnixMilli())
	return queryAndMapToInfos[gcr.PodInfo, podRow](d.selectScheduledPodsBeforeSnapshotTimestamp, timestamp, timestamp)
}

// GetLatestCADeployment needs a TODO: move me to generics
func (d *DataAccess) GetLatestCADeployment() (caDeployment *gcr.CASettingsInfo, err error) {
	rows, err := d.selectLatestCADeployment.Query()
	if err != nil {
		return
	}
	var caDeployments []gcr.CASettingsInfo
	err = scan.Rows(&caDeployments, rows)
	if err != nil {
		return nil, err
	}
	if len(caDeployments) == 0 {
		return nil, nil
	}
	caDeployment = &caDeployments[0]
	return
}

// GetCADeploymentWithHash has a  TODO: move me to generics
func (d *DataAccess) GetCADeploymentWithHash(Hash string) (caDeployment *gcr.CASettingsInfo, err error) {
	rows, err := d.selectLatestCADeployment.Query(Hash)
	if err != nil {
		return
	}
	var caDeployments []gcr.CASettingsInfo
	err = scan.Rows(&caDeployments, rows)
	if err != nil {
		return nil, err
	}
	if len(caDeployments) == 0 {
		return nil, nil
	}
	caDeployment = &caDeployments[0]
	return
}

func (d *DataAccess) StorePodInfo(podInfo gcr.PodInfo) (int64, error) {
	if podInfo.Hash == "" {
		podInfo.Hash = podInfo.GetHash()
	}
	labels, err := labelsToText(podInfo.Labels)
	if err != nil {
		return -1, err
	}
	requests, err := resourcesToText(podInfo.Requests)
	if err != nil {
		return -1, err
	}
	podSpec, err := specToJson(podInfo.Spec)
	if err != nil {
		return -1, err
	}
	result, err := d.insertPodInfo.Exec(
		podInfo.CreationTimestamp.UTC().UnixMilli(),
		podInfo.SnapshotTimestamp.UTC().UnixMilli(),
		podInfo.Name,
		podInfo.Namespace,
		podInfo.UID,
		podInfo.NodeName,
		podInfo.NominatedNodeName,
		labels,
		requests,
		podSpec,
		podInfo.PodScheduleStatus,
		podInfo.Hash)
	if err != nil {
		return -1, fmt.Errorf("could not persist podinfo %s: %w", podInfo, err)
	}
	slog.Info("stored row into pod_info.", "pod.Name", podInfo.Name, "pod.Namespace", podInfo.Namespace,
		"pod.CreationTimestamp", podInfo.CreationTimestamp, "pod.Hash", podInfo.Hash)
	return result.LastInsertId()
}

func (d *DataAccess) StoreNodeInfo(n gcr.NodeInfo) (rowID int64, err error) {
	if n.Hash == "" {
		n.Hash = n.GetHash()
	}
	// Removing this label as it just takes useless space: "node.machine.sapcloud.io/last-applied-anno-labels-taints"
	delete(n.Labels, "node.machine.sapcloud.io/last-applied-anno-labels-taints")
	labelsText, err := labelsToText(n.Labels)
	if err != nil {
		return
	}
	taintsText, err := taintsToText(n.Taints)
	if err != nil {
		return
	}
	allocatableText, err := resourcesToText(n.Allocatable)
	if err != nil {
		return
	}
	capacityText, err := resourcesToText(n.Capacity)
	if err != nil {
		return
	}
	_, err = d.insertNodeInfo.Exec(
		n.CreationTimestamp.UTC().UnixMilli(),
		n.SnapshotTimestamp.UTC().UnixMilli(),
		n.Name,
		n.Namespace,
		n.ProviderID,
		n.AllocatableVolumes,
		labelsText,
		taintsText,
		allocatableText,
		capacityText,
		n.Hash)
	if err != nil {
		slog.Error("cannot insert node_info in the node_info table", "error", err, "node", n)
		return
	}
	slog.Info("inserted new row into the node_info table", "node.Name", n.Name)
	return
}

func (d *DataAccess) LoadNodeInfosBefore(creationTimestamp time.Time) ([]gcr.NodeInfo, error) {
	nodeInfos, err := queryAndMapToInfos[gcr.NodeInfo, nodeRow](d.selectNodeInfosBefore, creationTimestamp)
	if err != nil {
		return nil, fmt.Errorf("LoadNodeInfosBefore could not scan rows: %w", err)
	}
	return nodeInfos, nil
}

func (d *DataAccess) StoreCADeployment(caSettings gcr.CASettingsInfo) (int64, error) {
	result, err := d.insertCADeployment.Exec(caSettings.Expander, caSettings.MaxNodesTotal, caSettings.Priorities, caSettings.Hash)
	if err != nil {
		return -1, err
	}
	return result.LastInsertId()
}

func (d *DataAccess) GetLatestNodesBeforeAndNotDeleted(timestamp time.Time) ([]gcr.NodeInfo, error) {
	nodeInfos, err := queryAndMapToInfos[gcr.NodeInfo, nodeRow](d.selectLatestNodesBeforeAndNotDeleted, timestamp)
	if err != nil {
		return nil, fmt.Errorf("GetLatestNodesBeforeAndNotDeleted could not scan rows: %w", err)
	}
	return nodeInfos, nil
}

func labelsToText(valMap map[string]string) (textVal string, err error) {
	if len(valMap) == 0 {
		return "", nil
	}
	bytes, err := json.Marshal(valMap)
	if err != nil {
		err = fmt.Errorf("cannot serialize labels %q due to: %w", valMap, err)
	} else {
		textVal = string(bytes)
	}
	return
}

func resourcesToText(resources corev1.ResourceList) (textVal string, err error) {
	if len(resources) == 0 {
		return "", nil
	}
	bytes, err := json.Marshal(resources)
	if err != nil {
		err = fmt.Errorf("cannot serialize resources %v due to: %w", resources, err)
	} else {
		textVal = string(bytes)
	}
	return
}

func tolerationsToText(tolerations []corev1.Toleration) (textVal string, err error) {
	if len(tolerations) == 0 {
		return "", nil
	}
	bytes, err := json.Marshal(tolerations)
	if err != nil {
		err = fmt.Errorf("cannot serialize tolerations %v due to: %w", tolerations, err)
	} else {
		textVal = string(bytes)
	}
	return
}

func tscToText(tsc []corev1.TopologySpreadConstraint) (textVal string, err error) {
	if len(tsc) == 0 {
		return "", nil
	}
	bytes, err := json.Marshal(tsc)
	if err != nil {
		err = fmt.Errorf("cannot serialize TopologySpreadConstraints %v due to: %w", tsc, err)
	} else {
		textVal = string(bytes)
	}
	return
}

func taintsToText(taints []corev1.Taint) (textVal string, err error) {
	if len(taints) == 0 {
		return "", nil
	}
	bytes, err := json.Marshal(taints)
	if err != nil {
		err = fmt.Errorf("cannot serialize taints %q due to: %w", taints, err)
	} else {
		textVal = string(bytes)
	}
	return
}

func labelsFromText(textValue string) (labels map[string]string, err error) {
	if strings.TrimSpace(textValue) == "" {
		return nil, nil
	}
	err = json.Unmarshal([]byte(textValue), &labels)
	if err != nil {
		err = fmt.Errorf("cannot de-serialize labels %q due to: %w", textValue, err)
	}
	return
}

func tolerationsFromText(textValue string) (tolerations []corev1.Toleration, err error) {
	if strings.TrimSpace(textValue) == "" {
		return nil, nil
	}
	err = json.Unmarshal([]byte(textValue), &tolerations)
	if err != nil {
		err = fmt.Errorf("cannot de-serialize tolerations %q due to: %w", textValue, err)
	}
	return
}

func tscFromText(textValue string) (tsc []corev1.TopologySpreadConstraint, err error) {
	if strings.TrimSpace(textValue) == "" {
		return nil, nil
	}
	err = json.Unmarshal([]byte(textValue), &tsc)
	if err != nil {
		err = fmt.Errorf("cannot de-serialize TopologySpreadConstraints %q due to: %w", textValue, err)
	}
	return
}

func specToJson(podSpec corev1.PodSpec) (textVal string, err error) {
	bytes, err := json.Marshal(podSpec)
	if err != nil {
		err = fmt.Errorf("cannot serialize podSpec %q due to: %w", podSpec.String(), err)
	} else {
		textVal = string(bytes)
	}
	return
}
func speccFromJson(jsonVal string) (podSpec corev1.PodSpec, err error) {
	if strings.TrimSpace(jsonVal) == "" {
		return
	}
	err = json.Unmarshal([]byte(jsonVal), &podSpec)
	if err != nil {
		err = fmt.Errorf("cannot de-serialize podSpec %q due to: %w", jsonVal, err)
	}
	return
}

func taintsFromText(textValue string) (taints []corev1.Taint, err error) {
	if strings.TrimSpace(textValue) == "" {
		return nil, nil
	}
	err = json.Unmarshal([]byte(textValue), &taints)
	if err != nil {
		err = fmt.Errorf("cannot de-serialize taints %q due to: %w", textValue, err)
	}
	return
}

func resourcesFromText(textValue string) (resources corev1.ResourceList, err error) {
	if strings.TrimSpace(textValue) == "" {
		return nil, nil
	}
	err = json.Unmarshal([]byte(textValue), &resources)
	if err != nil {
		err = fmt.Errorf("cannot de-serialize resources %q due to: %w", textValue, err)
	}
	return
}

func getHash(selectHashStmt *sql.Stmt, name string) (string, error) {
	row := selectHashStmt.QueryRow(name)
	var hash sql.NullString
	err := row.Scan(&hash)
	if hash.Valid {
		return hash.String, nil
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return "", nil

}

// queryAndMapToInfo executes the given prepared stmt with the given params and maps the rows to infoObjs which is a []I slice
func queryAndMapToInfos[I any, T row[I]](stmt *sql.Stmt, params ...any) (infoObjs []I, err error) {
	var rowObjs []T
	var rows *sql.Rows

	var adjustedParams = make([]any, len(params))
	for i, p := range params {
		adjustedParams[i] = adjustParam(p)
	}

	rows, err = stmt.Query(adjustedParams...)
	if err != nil {
		return
	}
	err = scan.Rows(&rowObjs, rows)
	if err != nil {
		return
	}
	if len(rowObjs) == 0 {
		return nil, sql.ErrNoRows
	}
	for _, r := range rowObjs {
		infoObj, err := r.AsInfo()
		if err != nil {
			return nil, err
		}
		infoObjs = append(infoObjs, infoObj)
	}
	return
}

func adjustParam(p any) any {
	if t, ok := p.(time.Time); ok {
		return t.UTC().UnixMilli()
	}
	return p
}

// queryAndMapToInfo executes the given prepared stmt with the given params and maps the first row to a single infoObj of type I
func queryAndMapToInfo[I any, T row[I]](stmt *sql.Stmt, param ...any) (infoObj I, err error) {
	rows, err := stmt.Query(param...)
	if err != nil {
		return
	}
	var rowObj T
	err = scan.Row(&rowObj, rows) //TODO: wrap err with msg and return
	if err != nil {
		err = fmt.Errorf("could not find rows for statement: %w", err)
		return
	}
	infoObj, err = rowObj.AsInfo()
	if err != nil {
		return
	}
	return infoObj, nil
}
