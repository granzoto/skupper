// +build integration acceptance console consolenato

package acceptance

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/skupperproject/skupper/client"
	"github.com/skupperproject/skupper/pkg/utils"
	"github.com/skupperproject/skupper/test/utils/constants"
	"gotest.tools/assert"
	"io"
	"io/ioutil"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"strings"
	"testing"
	"time"
)

var (
	PRIVCONSOLE        = ""
	PUBCONSOLE         = ""
	ADMUSER            = "admin"
	ADMPASS            = "admin"
	pubClaimCreated    corev1.Secret
	pubClaimDownloaded corev1.Secret
	firstLinkCreated   = ""
	firstClaimCreated  = ""
)

type TokenState struct {
	Name            string     `json:"name"`
	ClaimsMade      *int       `json:"claimsMade"`
	ClaimsRemaining *int       `json:"claimsRemaining"`
	ClaimExpiration *time.Time `json:"claimExpiration"`
	Created         string     `json:"created,omitempty"`
}

type LinkStatus struct {
	Name        string
	Url         string
	Cost        int
	Connected   bool
	Configured  bool
	Description string
	Created     string
}

type ServiceDefinition struct {
	Name      string            `json:"name"`
	Protocol  string            `json:"protocol"`
	Ports     []int             `json:"ports"`
	Endpoints []ServiceEndpoint `json:"endpoints"`
}

type ServiceEndpoint struct {
	Name   string      `json:"name"`
	Target string      `json:"target"`
	Ports  map[int]int `json:"ports,omitempty"`
}

type PortDescription struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

type ServiceTarget struct {
	Name  string            `json:"name"`
	Type  string            `json:"type"`
	Ports []PortDescription `json:"ports,omitempty"`
}

type ServiceOptions struct {
	Address     string            `json:"address"`
	Protocol    string            `json:"protocol"`
	Ports       []int             `json:"ports"`
	TargetPorts map[int]int       `json:"targetPorts,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Target      ServiceTarget     `json:"target"`
}

func accessConsole(method string, url string, path string, body io.Reader, user string, pass string) (string, error) {

	// Define the request first
	req, err := http.NewRequest(method, url+"/"+path, body)
	if err != nil {
		return "", fmt.Errorf("Error defining the request")
	}

	// If this is a POST
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}

	// Define request basic auth
	if user != "" {
		req.SetBasicAuth(user, pass)
	}

	// Define the HTTP Client
	client := http.Client{}

	if strings.HasPrefix(url, "https") {
		// Accept insecure connections
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Error sending request")
	}

	bodyResp, err := ioutil.ReadAll(resp.Body)

	strResp := string(bodyResp)
	if resp.StatusCode != http.StatusOK {
		return strResp, fmt.Errorf("Return code %d", resp.StatusCode)
	}

	return strResp, nil
}

func testAccessDATA(consoleUrl string) (string, error) {

	dataConsole, err := accessConsole("GET", consoleUrl, "DATA", nil, ADMUSER, ADMPASS)
	if err != nil {
		return "", fmt.Errorf("Unable to retrieve /DATA from %s", consoleUrl)
	}
	return dataConsole, nil
}

func getTokens(consoleUrl string) ([]TokenState, error) {

	tokensCreatedStr, err := accessConsole("GET", consoleUrl, "tokens", nil, ADMUSER, ADMPASS)
	if err != nil {
		return []TokenState{}, fmt.Errorf("Unable to list tokens from %s", consoleUrl)
	}

	var tokensCreated []TokenState
	err = json.Unmarshal([]byte(tokensCreatedStr), &tokensCreated)
	if err != nil {
		return []TokenState{}, fmt.Errorf("Unable to unmarshal tokens list for %s", consoleUrl)
	}
	return tokensCreated, nil
}

func createClaimToken(consoleUrl string, minutes int, uses int) (corev1.Secret, error) {

	tokenExpires := time.Now().Add(time.Minute * time.Duration(minutes)).Format(time.RFC3339)
	postPath := fmt.Sprintf("tokens?expiration=%s&uses=%d", tokenExpires, uses)

	tokenCreatedStr, err := accessConsole("POST", consoleUrl, postPath, nil, ADMUSER, ADMPASS)
	if err != nil {
		return corev1.Secret{}, fmt.Errorf("Unable to create token for %s", consoleUrl)
	}

	var tokenCreated corev1.Secret
	err = json.Unmarshal([]byte(tokenCreatedStr), &tokenCreated)

	if err != nil {
		return corev1.Secret{}, fmt.Errorf("Unable to unmarshal token for %s", consoleUrl)
	}
	return tokenCreated, nil
}

func getOneToken(consoleUrl string, claimID string) (TokenState, error) {

	getPath := fmt.Sprintf("tokens/%s", claimID)

	tokenGotStr, err := accessConsole("GET", consoleUrl, getPath, nil, ADMUSER, ADMPASS)
	if err != nil {
		return TokenState{}, fmt.Errorf("Unable to retrieve claim %s from %s", claimID, consoleUrl)
	}

	var tokenGot TokenState
	err = json.Unmarshal([]byte(tokenGotStr), &tokenGot)
	if err != nil {
		return TokenState{}, fmt.Errorf("Unable to unmarshal retrieved claim %s", claimID)
	}
	return tokenGot, nil
}

func downloadClaimToken(consoleUrl string, claimID string) (corev1.Secret, error) {

	postPath := fmt.Sprintf("downloadclaim/%s", claimID)

	tokenDownloadedStr, err := accessConsole("GET", consoleUrl, postPath, nil, ADMUSER, ADMPASS)
	if err != nil {
		return corev1.Secret{}, fmt.Errorf("Unable to download claim %s from %s", claimID, consoleUrl)
	}

	var tokenDownloaded corev1.Secret
	err = json.Unmarshal([]byte(tokenDownloadedStr), &tokenDownloaded)
	if err != nil {
		return corev1.Secret{}, fmt.Errorf("Unable to unmarshal downloaded claim %s", claimID)
	}
	return tokenDownloaded, nil
}

func lastSlice(fullString string, sep string) string {
	slicedString := strings.Split(fullString, sep)
	return string(slicedString[len(slicedString)-1])
}

// Links
func getLinks(consoleUrl string) ([]LinkStatus, error) {

	linksCreatedSTR, err := accessConsole("GET", consoleUrl, "links", nil, ADMUSER, ADMPASS)
	if err != nil {
		return []LinkStatus{}, fmt.Errorf("Unable to list links from %s", consoleUrl)
	}

	var linksCreated []LinkStatus
	err = json.Unmarshal([]byte(linksCreatedSTR), &linksCreated)
	if err != nil {
		return []LinkStatus{}, fmt.Errorf("Unable to unmarshal link list for %s", consoleUrl)
	}
	return linksCreated, nil
}

func getOneLink(consoleUrl string, linkID string) (LinkStatus, error) {

	getPath := fmt.Sprintf("links/%s", linkID)

	linkGotStr, err := accessConsole("GET", consoleUrl, getPath, nil, ADMUSER, ADMPASS)
	if err != nil {
		return LinkStatus{}, fmt.Errorf("Unable to retrieve link %s from %s", linkID, consoleUrl)
	}

	var linkGot LinkStatus
	err = json.Unmarshal([]byte(linkGotStr), &linkGot)
	if err != nil {
		return LinkStatus{}, fmt.Errorf("Unable to unmarshal retrieved link %s", linkID)
	}
	return linkGot, nil
}

func createLink(consoleUrl string, cost int, secret corev1.Secret) error {

	postPath := fmt.Sprintf("links?cost=%d", cost)
	secretSTR, err := json.Marshal(secret)
	if err != nil {
		return fmt.Errorf("Unable to unmarshal token for %s", consoleUrl)
	}

	_, err = accessConsole("POST", consoleUrl, postPath, bytes.NewReader(secretSTR), ADMUSER, ADMPASS)
	if err != nil {
		return fmt.Errorf("Unable to create token for %s - %s", consoleUrl, err)
	}
	return nil
}

//  Services
func getServices(consoleUrl string) ([]ServiceDefinition, error) {

	servicesStr, err := accessConsole("GET", consoleUrl, "services", nil, ADMUSER, ADMPASS)
	if err != nil {
		return []ServiceDefinition{}, fmt.Errorf("Unable to list services from %s", consoleUrl)
	}

	var services []ServiceDefinition
	err = json.Unmarshal([]byte(servicesStr), &services)
	if err != nil {
		return []ServiceDefinition{}, fmt.Errorf("Unable to unmarshal service list for %s", consoleUrl)
	}
	return services, nil
}

func createService(consoleUrl string, service ServiceOptions) error {

	postPath := "services"
	serviceSTR, err := json.Marshal(service)
	if err != nil {
		return fmt.Errorf("Unable to unmarshal service for %s", consoleUrl)
	}

	_, err = accessConsole("POST", consoleUrl, postPath, bytes.NewReader(serviceSTR), ADMUSER, ADMPASS)
	if err != nil {
		return fmt.Errorf("Unable to create service for %s", consoleUrl)
	}
	return nil
}

func getOneService(consoleUrl string, serviceID string) (ServiceDefinition, error) {

	getPath := fmt.Sprintf("services/%s", serviceID)

	serviceStr, err := accessConsole("GET", consoleUrl, getPath, nil, ADMUSER, ADMPASS)
	if err != nil {
		return ServiceDefinition{}, fmt.Errorf("Unable to retrieve service %s", serviceID)
	}

	var service ServiceDefinition
	err = json.Unmarshal([]byte(serviceStr), &service)
	if err != nil {
		return ServiceDefinition{}, fmt.Errorf("Unable to unmarshal service %s", serviceID)
	}
	return service, nil
}

// Check if a linkStatus structure contains an element based on its name
func findInSlice(elements []LinkStatus, key string) bool {

	for _, element := range elements {
		if element.Name == key {
			return true
		}
	}
	return false
}

func findNewLink(elementsAfter []LinkStatus, elementsBefore []LinkStatus) (string, error) {
	founds := 0
	newLink := ""
	for _, nameAfter := range elementsAfter {
		if findInSlice(elementsBefore, nameAfter.Name) == false {
			newLink = nameAfter.Name
			founds++
		}
	}
	if founds == 1 {
		return newLink, nil
	} else {
		return "", fmt.Errorf("More than 1 newLink found")
	}
}

func getTargets(consoleUrl string) ([]ServiceTarget, error) {

	getPath := fmt.Sprintf("targets")

	targetsStr, err := accessConsole("GET", consoleUrl, getPath, nil, ADMUSER, ADMPASS)
	if err != nil {
		return []ServiceTarget{}, fmt.Errorf("Unable to list targets from %s", consoleUrl)
	}

	var targets []ServiceTarget
	err = json.Unmarshal([]byte(targetsStr), &targets)
	if err != nil {
		return []ServiceTarget{}, fmt.Errorf("Unable to unmarshal targets from %s", consoleUrl)
	}
	return targets, nil
}

func getGenericEndpoint(consoleUrl string, getPath string) (string, error) {

	genEndPointStr, err := accessConsole("GET", consoleUrl, getPath, nil, ADMUSER, ADMPASS)
	if err != nil {
		return "", fmt.Errorf("Unable to retrieve endpoint %s", getPath)
	}
	return genEndPointStr, nil
}

// Create the deployment for the Backtend in public namespace
func CreateBackendDeployment(t *testing.T, cluster *client.VanClient) {
	name := "hello-world-backend"
	replicas := int32(1)
	dep := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels:    map[string]string{"app": name},
		},
		Spec: v1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  name,
							Image: "quay.io/skupper/" + name,
							Ports: []corev1.ContainerPort{{
								Name:          "8080",
								ContainerPort: 8080,
							}},
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		},
	}

	// Deploying resource
	dep, err := cluster.KubeClient.AppsV1().Deployments(cluster.Namespace).Create(dep)
	assert.Assert(t, err)
}

func delService(consoleUrl string, serviceName string) error {
	_, err := accessConsole("DELETE", consoleUrl, "services/"+serviceName, nil, ADMUSER, ADMPASS)
	if err != nil {
		return fmt.Errorf("Unable to delete service %s from %s", serviceName, consoleUrl)
	}
	return nil
}

func delLink(consoleUrl string, linkName string) error {
	_, err := accessConsole("DELETE", consoleUrl, "links/"+linkName, nil, ADMUSER, ADMPASS)
	if err != nil {
		return fmt.Errorf("Unable to delete link %s from %s", linkName, consoleUrl)
	}
	return nil
}

func delToken(consoleUrl string, tokenName string) error {
	_, err := accessConsole("DELETE", consoleUrl, "tokens/"+tokenName, nil, ADMUSER, ADMPASS)
	if err != nil {
		return fmt.Errorf("Unable to delete claim token %s from %s", tokenName, consoleUrl)
	}
	return nil
}

// Test Token Endpoints
func testTokensEndpoints(t *testing.T) {

	var err error

	t.Run("test-get-tokens-from-pub", func(t *testing.T) {
		tokensInPub, err := getTokens(PUBCONSOLE)
		assert.Assert(t, err, "Unable to retrieve token list from Public console")
		assert.Assert(t, len(tokensInPub) == 0)
	})

	t.Run("test-create-token-in-pub", func(t *testing.T) {
		pubClaimCreated, err = createClaimToken(PUBCONSOLE, 5, 2)
		// This will be used latter to test claim removal
		firstClaimCreated = pubClaimCreated.Name
		assert.Assert(t, err, "Unable to create a token in Public console")
		assert.Assert(t, pubClaimCreated.Name != "")
	})

	t.Run("test-retrieve-token-from-pub", func(t *testing.T) {
		tokenFromPub, err := getOneToken(PUBCONSOLE, pubClaimCreated.Name)
		assert.Assert(t, err, "Unable to retrieve tokens from Public console")
		assert.Assert(t, (lastSlice(pubClaimCreated.Annotations["skupper.io/url"], "/") == tokenFromPub.Name))
	})

	t.Run("test-download-token-from-pub", func(t *testing.T) {
		claimToDownload := lastSlice(pubClaimCreated.Annotations["skupper.io/url"], "/")
		pubClaimDownloaded, err = downloadClaimToken(PUBCONSOLE, claimToDownload)
		assert.Assert(t, err, "Unable to download a token from Public console")
		assert.Assert(t, pubClaimDownloaded.Name == claimToDownload)
	})

	t.Run("test-created-token-listed-in-pub", func(t *testing.T) {
		tokensInPub, err := getTokens(PUBCONSOLE)
		assert.Assert(t, err, "Unable to retrieve token list from Public console after token creation")
		assert.Assert(t, len(tokensInPub) == 1)
	})
}

// Test Links Endpoints
func testLinksEndpoints(t *testing.T) {

	var linksInPrivAfter []LinkStatus
	var linksInPrivBefore []LinkStatus
	var newLinkData LinkStatus
	var err error

	t.Run("test-get-links-from-priv", func(t *testing.T) {
		linksInPrivBefore, err = getLinks(PRIVCONSOLE)
		assert.Assert(t, err, "Unable to retrieve links list from Private console")
		assert.Assert(t, len(linksInPrivBefore) == 1)
	})

	t.Run("test-create-link-in-priv", func(t *testing.T) {
		err = createLink(PRIVCONSOLE, 4, pubClaimCreated)
		assert.Assert(t, err, "Unable to create a link in Private")
	})

	t.Run("test-get-links-after-creation", func(t *testing.T) {
		linksInPrivAfter, err = getLinks(PRIVCONSOLE)
		assert.Assert(t, err, "Unable to retrieve links list from Private console after link creation")
		assert.Assert(t, len(linksInPrivAfter) > 1, "Unable to retrieve links list from Private console after link creation")
	})

	newLink, err := findNewLink(linksInPrivAfter, linksInPrivBefore)
	if err == nil {
		firstLinkCreated = newLink
	}
	assert.Assert(t, err, "Unable to determine the created link name")

	t.Run("test-retrieve-one-link", func(t *testing.T) {

		ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute)
		defer cancelFn()

		err = utils.RetryWithContext(ctx, constants.DefaultTick, func() (bool, error) {

			newLinkData, err = getOneLink(PRIVCONSOLE, newLink)
			t.Log("[1RG-DEBUG] Err = ", err)
			t.Log("[2RG-DEBUG] NewLink = ", newLinkData.Connected)
			if err != nil {
				t.Log("[3RG-DEBUG ERR NOT NIL] Err = ", err)
				t.Log("[4RG-DEBUG ERR NOT NIL] NewLink = ", newLinkData.Connected)
				return true, err
			}
			t.Log("[5RG-DEBUG ERR IS NIL] Err = ", err)
			t.Log("[6RG-DEBUG ERR IS NIL] NewLink = ", newLinkData.Connected)
			return newLinkData.Connected, nil
		})

		assert.Assert(t, err, "Unable to retrieve details about the created link")
		assert.Assert(t, newLinkData.Name == newLink)
		assert.Assert(t, newLinkData.Configured)
		assert.Assert(t, newLinkData.Connected)
	})

	t.Run("test-uses-decrease-in-token", func(t *testing.T) {
		claimToGet := lastSlice(pubClaimCreated.Annotations["skupper.io/url"], "/")
		retrievedClaim, err := getOneToken(PUBCONSOLE, claimToGet)
		assert.Assert(t, err, "Unable to retrieve details about the claim used to create the link")
		assert.Assert(t, (*retrievedClaim.ClaimsRemaining == 1))
	})

	t.Run("test-create-second-link", func(t *testing.T) {
		err = createLink(PRIVCONSOLE, 3, pubClaimDownloaded)
		assert.Assert(t, err, "Unable to create a second link in Private")
	})

	// Adjust link lists
	linksInPrivBefore = linksInPrivAfter

	t.Run("test-get-links-after-second-creation", func(t *testing.T) {
		linksInPrivAfter, err = getLinks(PRIVCONSOLE)
		assert.Assert(t, err, "Unable to retrieve links list from Private console after second link creation")
		assert.Assert(t, len(linksInPrivAfter) > 0, "Unable to retrieve links list from Private console after second link creation")
	})

	newLink, err = findNewLink(linksInPrivAfter, linksInPrivBefore)
	assert.Assert(t, err, "Unable to determine the created link name")

	t.Run("test-retrieve-second-link", func(t *testing.T) {
		ctx, cancelFn := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancelFn()

		err = utils.RetryWithContext(ctx, constants.DefaultTick, func() (bool, error) {
			newLinkData, err = getOneLink(PRIVCONSOLE, newLink)
			if err != nil {
				fmt.Println("[RG-DEBUG] = ", err)
				return true, err
			}
			return newLinkData.Connected, nil
		})
		assert.Assert(t, err, "Unable to retrieve details about the second created link")
		assert.Assert(t, newLinkData.Name == newLink)
		assert.Assert(t, newLinkData.Configured)
		assert.Assert(t, newLinkData.Connected)
	})

	t.Run("test-uses-decrease-after-second-link", func(t *testing.T) {
		claimToGet := lastSlice(pubClaimCreated.Annotations["skupper.io/url"], "/")
		retrievedClaim, err := getOneToken(PUBCONSOLE, claimToGet)
		assert.Assert(t, err, "Unable to retrieve details about the claim used to create the two links")
		assert.Assert(t, (*retrievedClaim.ClaimsRemaining == 0))
	})

	// Adjust link lists
	linksInPrivBefore = linksInPrivAfter

	t.Run("test-create-third-link", func(t *testing.T) {
		err = createLink(PRIVCONSOLE, 2, pubClaimDownloaded)
		assert.Assert(t, err, "Unable to create a third link in Private")
	})

	t.Run("test-get-links-after-third-creation", func(t *testing.T) {
		linksInPrivAfter, err = getLinks(PRIVCONSOLE)
		assert.Assert(t, err, "Unable to retrieve links list from Private console after third link creation")
		assert.Assert(t, len(linksInPrivAfter) > 0, "Unable to retrieve links list from Private console after third link creation")
	})

	newLink, err = findNewLink(linksInPrivAfter, linksInPrivBefore)
	assert.Assert(t, err, "Unable to determine the created link name")

	t.Run("test-retrieve-third-link", func(t *testing.T) {
		newLinkData, err = getOneLink(PRIVCONSOLE, newLink)
		assert.Assert(t, err, "Unable to retrieve details about the second created link")
		assert.Assert(t, newLinkData.Name == newLink)
		assert.Assert(t, newLinkData.Configured == false)
		assert.Assert(t, newLinkData.Connected == false)
		assert.Assert(t, strings.Contains(newLinkData.Description, "Failed to redeem claim"))
	})
}

// Test Services and Targets
func testServicesEndpoints(t *testing.T) {

	t.Run("test-get-services-from-priv", func(t *testing.T) {
		svcsPriv, err := getServices(PRIVCONSOLE)
		assert.Assert(t, err, "Unable to retrieve services from Private")
		assert.Assert(t, len(svcsPriv) > 0)
	})

	// Service test requires a deployment
	privCluster, err := testRunner.GetPrivateContext(1)
	assert.Assert(t, err)
	CreateBackendDeployment(t, privCluster.VanClient)

	t.Run("test-create-service-in-priv", func(t *testing.T) {

		newsvc := ServiceOptions{
			Address:     "hello-world-backend",
			Protocol:    "http",
			Ports:       []int{8080},
			TargetPorts: map[int]int{8080: 8080},
			Labels:      nil,
			Target: ServiceTarget{
				Name: "hello-world-backend",
				Type: "deployment",
				Ports: []PortDescription{
					{Name: "8080",
						Port: 8080},
				},
			},
		}
		err := createService(PRIVCONSOLE, newsvc)
		assert.Assert(t, err, "Unable to create service hello-world-backend in Private")
	})

	t.Run("test-get-services-from-priv", func(t *testing.T) {
		svcsPriv, err := getServices(PRIVCONSOLE)
		assert.Assert(t, err, "Unable to retrieve services from Private")
		foundSvc := false
		for _, svc := range svcsPriv {
			if svc.Name == "hello-world-backend" {
				foundSvc = true
			}
		}
		assert.Assert(t, foundSvc == true)
	})

	t.Run("test-get-services-from-pub", func(t *testing.T) {
		svcsPub, err := getServices(PUBCONSOLE)
		assert.Assert(t, err, "Unable to retrieve services from Public")
		foundSvc := false
		for _, svc := range svcsPub {
			if svc.Name == "hello-world-backend" {
				foundSvc = true
			}
		}
		assert.Assert(t, foundSvc == true)
	})

	t.Run("test-get-one-specific-service", func(t *testing.T) {

		var oneService ServiceDefinition
		var err error

		ctx, cancelFn := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancelFn()

		err = utils.RetryWithContext(ctx, constants.DefaultTick, func() (bool, error) {
			oneService, err = getOneService(PRIVCONSOLE, "hello-world-backend")
			if err != nil {
				fmt.Println("[RG-DEBUG] = ", err)
				return true, err
			}
			return oneService.Name == "hello-world-backend", nil
		})
		assert.Assert(t, err, "Unable to retrieve details about service hello-world-backend")
		assert.Assert(t, oneService.Name == "hello-world-backend")
	})

	t.Run("test-get-targets-from-service", func(t *testing.T) {
		var targetsInSvc []ServiceTarget
		var err error

		ctx, cancelFn := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancelFn()

		err = utils.RetryWithContext(ctx, constants.DefaultTick, func() (bool, error) {
			targetsInSvc, err = getTargets(PRIVCONSOLE)
			if err != nil {
				fmt.Println("[RG-DEBUG] = ", err)
				return true, err
			}

			var foundTrgt = false
			for _, trgt := range targetsInSvc {
				if trgt.Name == "hello-world-backend" {
					foundTrgt = true
				}
			}
			return foundTrgt, nil
		})
		assert.Assert(t, err, "Unable to retrieve targets from service hello-world-backend")

		var foundTrgt = false
		for _, trgt := range targetsInSvc {
			if trgt.Name == "hello-world-backend" {
				foundTrgt = true
			}
		}
		assert.Assert(t, foundTrgt == true)
	})

	t.Run("test-service-check", func(t *testing.T) {
		chkSvc, err := getGenericEndpoint(PRIVCONSOLE, "servicecheck/hello-world-backend")
		assert.Assert(t, err, "Unable to check service service hello-world-backend")
		assert.Assert(t, chkSvc != "")
	})
}

func testGeneralEndpoints(t *testing.T) {

	t.Run("test-get-version", func(t *testing.T) {
		version, err := getGenericEndpoint(PUBCONSOLE, "version")
		assert.Assert(t, err, "Unable to check Skupper version")
		assert.Assert(t, version != "")
	})

	t.Run("test-get-site", func(t *testing.T) {
		site, err := getGenericEndpoint(PUBCONSOLE, "site")
		assert.Assert(t, err, "Unable to check Site ID")
		assert.Assert(t, site != "")
	})

	t.Run("test-get-events", func(t *testing.T) {
		events, err := getGenericEndpoint(PUBCONSOLE, "events")
		assert.Assert(t, err, "Unable to retrieve events")
		assert.Assert(t, events != "")
	})
}

func testRemoveResources(t *testing.T) {

	t.Run("test-remove-service hello-world-private", func(t *testing.T) {
		err := delService(PRIVCONSOLE, "hello-world-backend")
		assert.Assert(t, err, "Unable to remove service hello-world-backend")
	})

	t.Run("test-check-services-removed", func(t *testing.T) {
		svcsPriv, err := getServices(PRIVCONSOLE)
		assert.Assert(t, err, "Unable to retrieve services from Private")
		for _, svc := range svcsPriv {
			if svc.Name == "hello-world-backend" {
				t.Log("FAIL : Service hello-world-backend was not removed")
				t.Fail()
			}
		}
		assert.Assert(t, true)
	})

	t.Run("test-remove-link", func(t *testing.T) {
		if firstLinkCreated != "" {
			err := delLink(PRIVCONSOLE, firstLinkCreated)
			assert.Assert(t, err, "Unable to remove first link created")
		} else {
			t.Skip("Unable to determine the link to be removed. Skipping")
		}
	})

	t.Run("test-remove-token", func(t *testing.T) {
		if firstClaimCreated != "" {
			err := delToken(PRIVCONSOLE, firstClaimCreated)
			assert.Assert(t, err, "Unable to remove first token created")
		} else {
			t.Skip("Unable to determine the claim token to be removed. Skipping")
		}
	})
}

func TestConsoleEndpointsExternal(t *testing.T) {

	// Get context for Public
	pubCluster, err := testRunner.GetPublicContext(1)
	assert.Assert(t, err)

	// Get context for Private
	privCluster, err := testRunner.GetPrivateContext(1)
	assert.Assert(t, err)

	pubCli, err := pubCluster.VanClient.RouterInspect(context.Background())
	assert.Assert(t, err, "Unable to retrieve Public Console URL")
	PUBCONSOLE = pubCli.ConsoleUrl

	privCli, err := privCluster.VanClient.RouterInspect(context.Background())
	assert.Assert(t, err, "Unable to retrieve Private Console URL")
	PRIVCONSOLE = privCli.ConsoleUrl

	fmt.Println("[RG] Pub URL = ", PUBCONSOLE)
	fmt.Println("[RG] Priv URL = ", PRIVCONSOLE)

	// Test Tokens
	testTokensEndpoints(t)
	if t.Failed() {
		return
	}

	// Test Links
	testLinksEndpoints(t)
	if t.Failed() {
		return
	}

	// Test Services and Targets
	testServicesEndpoints(t)
	if t.Failed() {
		return
	}

	// Test General Endpoints
	testGeneralEndpoints(t)
	if t.Failed() {
		return
	}

	// Test Removing services
	testRemoveResources(t)
	if t.Failed() {
		return
	}

}
