package io.textile.threads;

import java.util.Arrays;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import android.arch.lifecycle.LifecycleObserver;
import com.google.protobuf.ByteString;
import io.grpc.CallCredentials;
import io.grpc.ManagedChannel;
import io.grpc.stub.StreamObserver;
import io.textile.threads_grpc.*;

/**
 * Provides top level access to the Textile API
 */
public class Client implements LifecycleObserver {
    private static APIGrpc.APIBlockingStub blockingStub;
    private static APIGrpc.APIStub asyncStub;
    private static Config config = new DefaultConfig();

    private ExecutorService executor
            = Executors.newSingleThreadExecutor();

    enum ClientState {
        Connected, Idle
    }
    public static ClientState state = ClientState.Idle;

    /**
     * Initialize a new Client
     */
    public Client() {
    }

    /**
     * Initialize a new Client
     * @param config is either a DefaultConfig for running threadsd or TextileConfig for using hosted.
     */
    public Client(Config config) {
        this.config = config;
    }

    /**
     *
     * @return the current session id or null
     */
    public String getSession() {
        return this.config.getSession();
    }
    /**
     * Method must be called before using the Client and while the device has an internet connection.
     */
    public Future<Void> init() throws Exception {
        return executor.submit(() -> {
            config.init();
            String session = config.getSession();
            ManagedChannel channel = config.getChannel();
            if (session != null) {
                CallCredentials bearer = new BearerToken(session);
                blockingStub = APIGrpc.newBlockingStub(channel)
                        .withCallCredentials(bearer);
                asyncStub = APIGrpc.newStub(channel)
                        .withCallCredentials(bearer);
            } else {
                blockingStub = APIGrpc.newBlockingStub(channel);
                asyncStub = APIGrpc.newStub(channel);
            }
            state = ClientState.Connected;
            return null;
        });
    }

    public void NewDBSync (Credentials creds) {
        NewDBRequest.Builder request = NewDBRequest.newBuilder();
        request.setCredentials(creds);
        blockingStub.newDB(request.build());
    }

    public void NewDB (Credentials creds, StreamObserver<NewDBReply> responseObserver) {
        NewDBRequest.Builder request = NewDBRequest.newBuilder();
        request.setCredentials(creds);
        asyncStub.newDB(request.build(), responseObserver);
    }

    public void NewDBFromAddrSync (Credentials creds, String address, ByteString key, List<CollectionConfig> collections) {
        NewDBFromAddrRequest.Builder request = NewDBFromAddrRequest.newBuilder();
        request.setCredentials(creds);
        request.setAddr(address);
        request.setKey(key);
        for (int i = 0; i < collections.size(); i++) {
            request.setCollections(i, collections.get(i));
        }
        blockingStub.newDBFromAddr(request.build());
    }

    public void NewDBFromAddr (Credentials creds, String address, ByteString key, List<CollectionConfig> collections, StreamObserver<NewDBReply> responseObserver) {
        NewDBFromAddrRequest.Builder request = NewDBFromAddrRequest.newBuilder();
        request.setCredentials(creds);
        request.setAddr(address);
        request.setKey(key);
        for (int i = 0; i < collections.size(); i++) {
            request.setCollections(i, collections.get(i));
        }
        asyncStub.newDBFromAddr(request.build(), responseObserver);
    }

    public GetDBInfoReply GetDBInfoSync (Credentials creds) {
        GetDBInfoRequest.Builder request = GetDBInfoRequest.newBuilder();
        request.setCredentials(creds);
        return blockingStub.getDBInfo(request.build());
    }

    public void GetDBInfo (Credentials creds, StreamObserver<GetDBInfoReply> responseObserver) {
        GetDBInfoRequest.Builder request = GetDBInfoRequest.newBuilder();
        request.setCredentials(creds);
        asyncStub.getDBInfo(request.build(), responseObserver);
    }

    public CreateReply CreateSync (Credentials creds, String collectionName, ByteString[] instances) {
        CreateRequest.Builder request = CreateRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        request.addAllInstances(Arrays.asList(instances));
        CreateReply reply = blockingStub.create(request.build());
        return reply;
    }

    public void Create (Credentials creds, String collectionName, ByteString[] instances, StreamObserver<CreateReply> responseObserver) {
        CreateRequest.Builder request = CreateRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        request.addAllInstances(Arrays.asList(instances));
        asyncStub.create(request.build(), responseObserver);
    }

    public SaveReply SaveSync (Credentials creds, String collectionName, ByteString[] instances) {
        SaveRequest.Builder request = SaveRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        request.addAllInstances(Arrays.asList(instances));
        SaveReply reply = blockingStub.save(request.build());
        return reply;
    }

    public void Save (Credentials creds, String collectionName, ByteString[] instances, StreamObserver<SaveReply> responseObserver) {
        SaveRequest.Builder request = SaveRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        request.addAllInstances(Arrays.asList(instances));
        SaveReply reply = blockingStub.save(request.build());
        asyncStub.save(request.build(), responseObserver);
    }

    public boolean HasSync (Credentials creds, String collectionName, String[] instanceIDs) {
        HasRequest.Builder request = HasRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        for (int i = 1; i < instanceIDs.length; i++) {
            request.setInstanceIDs(i, instanceIDs[i]);
        }
        HasReply reply = blockingStub.has(request.build());
        return reply.getExists();
    }

    public void Has (Credentials creds, String collectionName, String[] instanceIDs, StreamObserver<HasReply> responseObserver) {
        HasRequest.Builder request = HasRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        for (int i = 1; i < instanceIDs.length; i++) {
            request.setInstanceIDs(i, instanceIDs[i]);
        }
        asyncStub.has(request.build(), responseObserver);
    }

    public FindByIDReply FindByIDSync (Credentials creds, String collectionName, String instanceID) {
        FindByIDRequest.Builder request = FindByIDRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        request.setInstanceID(instanceID);
        FindByIDReply reply = blockingStub.findByID(request.build());
        return reply;
    }
  
    public void FindByID (Credentials creds, String collectionName, String instanceID, StreamObserver<FindByIDReply> responseObserver) {
        FindByIDRequest.Builder request = FindByIDRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        request.setInstanceID(instanceID);
        asyncStub.findByID(request.build(), responseObserver);
    }

    public FindReply FindSync (Credentials creds, String collectionName, ByteString query) {
        FindRequest.Builder request = FindRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        request.setQueryJSON(query);
        FindReply reply = blockingStub.find(request.build());
        return reply;
    }

    public void Find (Credentials creds, String collectionName, ByteString query, StreamObserver<FindReply> responseObserver) {
        FindRequest.Builder request = FindRequest.newBuilder();
        request.setCredentials(creds);
        request.setCollectionName(collectionName);
        request.setQueryJSON(query);
        asyncStub.find(request.build(), responseObserver);
    }

    public void NewCollectionSync (Credentials creds, String name, ByteString schema) {
        NewCollectionRequest.Builder request = NewCollectionRequest.newBuilder();
        request.setCredentials(creds);
        CollectionConfig.Builder config = CollectionConfig.newBuilder();
        config.setName(name);
        config.setSchema(schema);
        request.setConfig(config);
        blockingStub.newCollection(request.build());
    }

    public void NewCollection (Credentials creds, String name, ByteString schema, StreamObserver<NewCollectionReply> responseObserver) {
        NewCollectionRequest.Builder request = NewCollectionRequest.newBuilder();
        request.setCredentials(creds);
        CollectionConfig.Builder config = CollectionConfig.newBuilder();
        config.setName(name);
        config.setSchema(schema);
        request.setConfig(config);
        asyncStub.newCollection(request.build(), responseObserver);
    }

    public Boolean connected() {
        return state == ClientState.Connected;
    }
}
